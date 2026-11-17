// SPDX-License-Identifier: GPL-3.0-only

package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"netrave/internal/api"
	"netrave/internal/auth"
	"netrave/internal/config"
	"netrave/internal/console"
	"netrave/internal/database"
	"netrave/internal/devices"
	"netrave/internal/folders"
	"netrave/internal/hoststat"
	"netrave/internal/iconlib"
	"netrave/internal/icons"
	"netrave/internal/images"
	"netrave/internal/ksm"
	"netrave/internal/labs"
	"netrave/internal/network"
	"netrave/internal/notify"
	"netrave/internal/qemu"
	"netrave/internal/roles"
	"netrave/internal/version"
	"netrave/internal/web"
)

// Version is kept as a package variable for the existing API status payload.
var Version = version.Version

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; style-src 'self'; script-src 'self'; connect-src 'self' ws: wss:; frame-ancestors 'none'")
		if len(r.URL.Path) < 5 || r.URL.Path[:5] != "/api/" {
			h.Set("Cache-Control", "no-cache")
		} else {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// Run starts the system and blocks until SIGINT/SIGTERM is received.
func Run(cfg *config.Config) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()
	if err := db.AttachFolders(filepath.Join(cfg.LabsDir, "folders.yaml")); err != nil {
		return fmt.Errorf("folders: %w", err)
	}
	if err := db.AttachLabs(cfg.LabsDir); err != nil {
		return fmt.Errorf("labs: %w", err)
	}

	as := auth.New(db)
	as.UseConfig(cfg)
	if pw, created, err := as.BootstrapAdmin(); err != nil {
		return fmt.Errorf("bootstrap administrator: %w", err)
	} else if created {
		log.Printf("============================================================")
		log.Printf("Created administrator: admin / %s", pw)
		log.Printf("Save this password and change it after the first login (shown once).")
		log.Printf("============================================================")
	}

	notifySvc := notify.New(db)
	for _, c := range Preflight(cfg.QEMUPath) {
		if c.OK {
			log.Printf("[ok]   %-12s %s", c.Name, c.Detail)
		} else {
			log.Printf("[FAIL] %-12s %s", c.Name, c.Detail)
			notifySvc.SendAdmins(notify.Warning, "system", "Environment check failed",
				c.Name+": "+c.Detail, "/dashboard")
		}
	}

	nm := network.New(db, cfg)
	mgr := qemu.New(db, cfg, nm)
	nm.SetQEMU(mgr)
	mgr.Notify = notifySvc
	mgr.Recover()
	reconcile := func() {
		for _, e := range nm.Reconcile() {
			log.Printf("network: %v", e)
			notifySvc.SendAdmins(notify.Warning, "network", "Network error", fmt.Sprint(e), "/dashboard")
		}
	}
	reconcile()
	// A full reconciliation of host interfaces must not delay the HTTP response to wire creation/deletion:
	// it is coalesced and runs in the background, no more than one pass at a time.
	netChanged := make(chan struct{}, 1)
	go func() {
		for range netChanged {
			reconcile()
		}
	}()

	started := time.Now()
	cpu := hoststat.New()
	h := &api.Handler{
		DB:     db,
		Auth:   as,
		Cfg:    cfg,
		CPU:    cpu,
		Notify: notifySvc,
		M:      mgr,
		Status: func() map[string]any {
			memTotal, memAvail := memInfo()
			diskFree, diskTotal := diskInfo(cfg.DataDir)
			// KSM/UKSM memory merging: how much memory is not additionally used thanks to it.
			merging := ksm.Read()
			return map[string]any{
				"version":        Version,
				"uptime_sec":     int64(time.Since(started).Seconds()),
				"checks":         Preflight(cfg.QEMUPath),
				"mem_total":      memTotal,
				"mem_avail":      memAvail,
				"disk_free":      diskFree,
				"disk_total":     diskTotal,
				"data_dir":       cfg.DataDir,
				"images_dir":     cfg.ImagesDir,
				"labs_dir":       cfg.LabsDir,
				"listen_addr":    cfg.Addr,
				"qemu_binary":    cfg.QEMUPath,
				"ksm_enabled":    merging.KSM.Enabled,
				"ksm_available":  merging.KSM.Available,
				"ksm_saved":      merging.KSM.SavedBytes,
				"uksm_enabled":   merging.UKSM.Enabled,
				"uksm_available": merging.UKSM.Available,
				"uksm_saved":     merging.UKSM.SavedBytes,
			}
		},
	}

	mux := http.NewServeMux()
	h.Register(mux)
	iconStore := icons.New(cfg.IconsDir)
	labsH := &labs.Handler{
		DB: db, Auth: as, M: mgr, Notify: notifySvc, Icons: iconStore,
		Cleanup: func(labID int64, ids []int64) {
			// ids - topology ids; machine directories are named after instance rids, so cleanup is by the database
			mgr.PruneLab(labID)
		},
		LabRemoved: mgr.RemoveLabDir,
		NetChanged: func() {
			select {
			case netChanged <- struct{}{}:
			default: // reconciliation is already scheduled
			}
		},
		WipeDevice: mgr.Wipe,
		HasDisks:   mgr.HasDisks,
		NICState:   nm.NICStates,
		Stats: func(rids []int64) map[int64]labs.MachineStat {
			out := map[int64]labs.MachineStat{}
			for id, st := range mgr.Stats(rids) {
				out[id] = labs.MachineStat{CPU: st.CPU, RSS: st.RSS, Ready: st.Ready}
			}
			return out
		},
		CableSet: func(instID, connID int64, up bool) error {
			return nm.SetCable(instID, connID, up, mgr.SetNICLink)
		},
		CableAttach: func(connID int64) []error {
			return nm.AttachLive(connID, mgr.SetNICLink, mgr.HotplugNIC)
		},
		CablePlan: nm.PlanDetach,
		CableDetach: func(plugs []network.Plug) []error {
			return nm.Detach(plugs, mgr.SetNICLink, mgr.UnplugNIC)
		},
		NetworkDiag: nm.Diagnostics,
		StopLab: func(labID int64) error {
			return mgr.StopLab(labID, 60*time.Second)
		},
	}
	labsH.Register(mux)
	h.DropUser = labsH.DropUser
	(&images.Handler{DB: db, Auth: as, Cfg: cfg, Notify: notifySvc, M: mgr}).Register(mux)
	(&iconlib.Handler{DB: db, Auth: as, Icons: iconStore}).Register(mux)
	notifySvc.Register(mux, as)
	(&folders.Handler{DB: db, Auth: as, Notify: notifySvc}).Register(mux)
	(&roles.Handler{DB: db, Auth: as, Cfg: cfg}).Register(mux)
	(&devices.Handler{DB: db, Auth: as, M: mgr, Notify: notifySvc}).Register(mux)
	console.New(db, as, mgr).Register(mux)
	mux.Handle("/", web.SPAHandler(web.FS()))

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go mgr.Run(ctx)
	go cpu.Run(ctx)
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				as.Cleanup()
			}
		}
	}()
	// Icons not bound to machines (unfinished uploads, deleted machines) are removed after an hour.
	go func() {
		sweep := func() {
			if err := iconStore.Sweep(db, time.Hour); err != nil {
				log.Printf("icon sweep: %v", err)
			}
		}
		sweep()
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweep()
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on %s (data: %s)", cfg.Addr, cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Printf("shutdown signal received")
	shCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = srv.Shutdown(shCtx)
	if cfg.StopVMsOnExit {
		log.Printf("stopping virtual machines...")
		mgr.StopAll(60 * time.Second)
	} else {
		log.Printf("virtual machines keep running and will be reattached on the next start")
	}
	return err
}
