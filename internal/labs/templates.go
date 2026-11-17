// SPDX-License-Identifier: GPL-3.0-only

package labs

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"netrave/internal/auth"
	"netrave/internal/catalog"
	"netrave/internal/database"
	"netrave/internal/httpx"
	"netrave/internal/icons"
	"netrave/internal/labfile"
	"netrave/internal/names"
	"netrave/internal/notify"
)

type Template struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Vendor   string `json:"vendor"`
	Type     string `json:"type"`
	Category string `json:"category"`
	Params
	Icon string `json:"icon"`
	// ImageID - the disk (image) that new machines from the template get by default (0 - none).
	ImageID int64 `json:"image_id"`
	// CustomIcon - hash of the machine's custom icon (empty - the standard icon or the type icon).
	CustomIcon string `json:"custom_icon"`
	// CustomIconURL - URL of the custom icon for the UI; empty - the standard one.
	CustomIconURL string `json:"custom_icon_url,omitempty"`
	// VNC - default VNC console display settings for machines from the template.
	VNC catalog.VNCQuality `json:"vnc"`
}

// tplFromCatalog assembles a template for the client from a catalog record.
func tplFromCatalog(ct catalog.Template) *Template {
	t := &Template{
		ID: ct.ID, Name: ct.Name, Vendor: ct.Vendor, Type: ct.Type, Category: ct.Category,
		Params: Params{
			Arch: ct.Arch, Machine: ct.Machine, CPU: ct.CPU, RAMMB: ct.RAMMB, NICs: ct.NICs, NICModel: ct.NICModel,
			IfaceType: ct.IfaceType, IfaceStart: ct.IfaceStart, DiskBus: ct.DiskBus, BIOS: ct.BIOS, Console: ct.Console, ExtraArgs: ct.ExtraArgs,
		},
		Icon: ct.Icon, ImageID: ct.ImageID, CustomIcon: ct.CustomIcon, VNC: ct.VNC.Normalized(),
	}
	if t.CustomIcon != "" {
		t.CustomIconURL = "/api/templates/" + strconv.FormatInt(t.ID, 10) + "/icons/" + t.CustomIcon
	}
	if t.Category == "" {
		t.Category = database.CategoryFor(t.Vendor, t.Type)
	}
	// Naming is stored per machine; a template only carries the default prefix.
	t.Normalize()
	return t
}

// tplFields - template fields for writing to the catalog (without the id).
func tplFields(in templateIn, imageID int64, icon, custom string) catalog.Template {
	return catalog.Template{
		Name: in.Name, Vendor: in.Vendor, Type: in.Type, Category: in.Category,
		Arch: in.Arch, Machine: in.Machine, CPU: in.CPU, RAMMB: in.RAMMB, NICs: in.NICs, NICModel: in.NICModel,
		IfaceType: in.IfaceType, IfaceStart: in.IfaceStart, DiskBus: in.DiskBus, BIOS: in.BIOS, Console: in.Console,
		ExtraArgs: in.ExtraArgs, ImageID: imageID, Icon: icon, CustomIcon: custom, VNC: in.VNC,
	}
}

// catalogOf - fields of an existing template for a copy.
func catalogOf(t *Template) catalog.Template {
	return catalog.Template{
		Name: t.Name, Vendor: t.Vendor, Type: t.Type, Category: t.Category,
		Arch: t.Arch, Machine: t.Machine, CPU: t.CPU, RAMMB: t.RAMMB, NICs: t.NICs, NICModel: t.NICModel,
		IfaceType: t.IfaceType, IfaceStart: t.IfaceStart, DiskBus: t.DiskBus, BIOS: t.BIOS, Console: t.Console,
		ExtraArgs: t.ExtraArgs, ImageID: t.ImageID, Icon: t.Icon, CustomIcon: t.CustomIcon, VNC: t.VNC,
	}
}

// templateWriteErr translates a catalog error into an API response.
func templateWriteErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, catalog.ErrExists):
		httpx.Err(w, http.StatusConflict, "шаблон с таким названием уже существует")
	case errors.Is(err, catalog.ErrNotFound):
		httpx.Err(w, http.StatusNotFound, "шаблон не найден")
	default:
		httpx.Err(w, http.StatusBadRequest, err.Error())
	}
}

func getTemplate(cat *catalog.Store, id int64) (*Template, error) {
	ct, ok := cat.Template(id)
	if !ok {
		return nil, errNotFound
	}
	return tplFromCatalog(ct), nil
}

func (h *Handler) listTemplates(w http.ResponseWriter, r *http.Request) {
	cts := h.DB.Catalog.Templates()
	sort.SliceStable(cts, func(i, j int) bool { return cts[i].Name < cts[j].Name })
	out := []*Template{}
	for _, ct := range cts {
		out = append(out, tplFromCatalog(ct))
	}
	httpx.JSON(w, http.StatusOK, out)
}

type templateIn struct {
	Name     string `json:"name"`
	Vendor   string `json:"vendor"`
	Type     string `json:"type"`
	Category string `json:"category"`
	Params
	// The template's disk and icon (optional): the default image and the machine icon.
	ImageID    *int64 `json:"image_id"`
	Icon       string `json:"icon"`
	CustomIcon string `json:"custom_icon"`
	// VNC - the template's VNC console settings (used when the console is vnc).
	VNC catalog.VNCQuality `json:"vnc"`
}

func (in *templateIn) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Vendor = strings.TrimSpace(in.Vendor)
	if err := names.Check("название шаблона", in.Name); err != nil {
		return err
	}
	if utf8.RuneCountInString(in.Vendor) > 64 {
		return errors.New("производитель: не более 64 символов")
	}
	if !tplTypes[in.Type] {
		return errors.New("недопустимый тип шаблона")
	}
	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = database.CategoryFor(in.Vendor, in.Type)
	} else if !database.ValidCategory(in.Category) {
		return errors.New("недопустимая категория шаблона")
	}
	in.VNC = in.VNC.Normalized()
	if err := in.VNC.Validate(); err != nil {
		return err
	}
	return in.Params.Validate()
}

// templateIconNames - standard machine icons that can be selected for a template.
// The set is the same as for machines (DeviceIcons).
var templateIconNames = DeviceIcons

// templateAssets validates the template's disk and icon: the image must exist, the icon must be
// a known standard one or a previously uploaded image. Returns the values for writing to the database.
func (h *Handler) templateAssets(in templateIn) (int64, string, string) {
	var imageID int64
	if in.ImageID != nil && *in.ImageID > 0 {
		if _, ok := h.DB.Catalog.Image(*in.ImageID); ok {
			imageID = *in.ImageID
		}
	}
	icon := strings.TrimSpace(in.Icon)
	if !templateIconNames[icon] {
		icon = ""
	}
	custom := strings.TrimSpace(in.CustomIcon)
	if custom != "" && !icons.ValidHash(custom) {
		custom = ""
	}
	return imageID, icon, custom
}

func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) {
	var in templateIn
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	if err := in.validate(); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	imageID, icon, custom := h.templateAssets(in)
	id, err := h.DB.Catalog.AddTemplate(tplFields(in, imageID, icon, custom))
	if err != nil {
		templateWriteErr(w, err)
		return
	}
	h.audit(r, "template_create", in.Name)
	h.notifyTemplates(r, "Шаблон создан", in.Name)
	t, _ := getTemplate(h.DB.Catalog, id)
	httpx.JSON(w, http.StatusCreated, t)
}

// duplicateTemplate - POST /api/templates/{id}/duplicate: a copy of the template with all parameters,
// disk and icon. The copy name gets the suffix "(copy)" or a number if that name is already taken.
func (h *Handler) duplicateTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	src, err := getTemplate(h.DB.Catalog, id)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "шаблон не найден")
		return
	}
	name := copyTemplateName(src.Name)
	fields := catalogOf(src)
	var newID int64
	for i := 2; i <= 50; i++ {
		fields.Name = name
		newID, err = h.DB.Catalog.AddTemplate(fields)
		if err == nil || !errors.Is(err, catalog.ErrExists) {
			break
		}
		name = copyTemplateNameWithNumber(src.Name, i)
	}
	if err != nil {
		templateWriteErr(w, err)
		return
	}
	h.audit(r, "template_duplicate", src.Name+" → "+name)
	h.notifyTemplates(r, "Шаблон скопирован", name)
	t, _ := getTemplate(h.DB.Catalog, newID)
	httpx.JSON(w, http.StatusCreated, t)
}

// copyTemplateName returns the copy name: "Name (copy)", no longer than 64 characters.
func copyTemplateName(base string) string {
	return copyTemplateNameWithNumber(base, 0)
}

// copyTemplateNameWithNumber builds the copy name in Latin: base-copy, base-copy-2, etc.
func copyTemplateNameWithNumber(base string, n int) string {
	suffix := "-copy"
	if n > 1 {
		suffix = "-copy-" + strconv.Itoa(n)
	}
	b := strings.TrimSpace(base)
	if limit := names.MaxLen - len(suffix); len(b) > limit {
		b = b[:limit]
	}
	return b + suffix
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	var in templateIn
	if !httpx.Decode(w, r, &in, 1<<20) {
		return
	}
	if err := in.validate(); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	imageID, icon, custom := h.templateAssets(in)
	nf := tplFields(in, imageID, icon, custom)
	if err := h.DB.Catalog.UpdateTemplate(id, func(ct *catalog.Template) error {
		*ct = nf
		ct.ID = id
		return nil
	}); err != nil {
		templateWriteErr(w, err)
		return
	}
	h.audit(r, "template_update", in.Name)
	h.notifyTemplates(r, "Шаблон изменён", in.Name)
	t, _ := getTemplate(h.DB.Catalog, id)
	httpx.JSON(w, http.StatusOK, t)
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "некорректный id")
		return
	}
	t, err := getTemplate(h.DB.Catalog, id)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "шаблон не найден")
		return
	}
	if err := h.DB.Catalog.DeleteTemplate(id); err != nil {
		templateWriteErr(w, err)
		return
	}
	if err := clearTemplateRefs(h.DB, id); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "не удалось обновить лаборатории")
		return
	}
	h.audit(r, "template_delete", t.Name)
	h.notifyTemplates(r, "Шаблон удалён", t.Name)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// notifyTemplates notifies all active users except the author about a change in the template catalog.
func (h *Handler) notifyTemplates(r *http.Request, title, name string) {
	actor := int64(0)
	if ss := auth.FromContext(r.Context()); ss != nil {
		actor = ss.User.ID
	}
	h.Notify.SendActive(actor, notify.Info, "template", title, "Шаблон «"+name+"» в каталоге.", "/templates")
}

// clearTemplateRefs removes the reference to a deleted template from lab topologies. Previously this was
// done by the ON DELETE SET NULL foreign key; now the topology is in files, and the nulling is done here.
func clearTemplateRefs(db *database.DB, tplID int64) error {
	for _, l := range db.Labs.Labs() {
		hit := false
		for _, d := range l.Devices {
			if d.TemplateID == tplID {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		if err := updateLab(db, l.ID, func(fl *labfile.Lab) error {
			for i := range fl.Devices {
				if fl.Devices[i].TemplateID == tplID {
					fl.Devices[i].TemplateID = 0
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
