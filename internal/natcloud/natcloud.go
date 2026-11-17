// SPDX-License-Identifier: GPL-3.0-only

// Package natcloud describes the server's shared NAT cloud: one bridge, one gateway, one DHCP pool and one masquerade
// for all labs and all users. The address configuration is shared and is not configured per network,
// so IP address conflicts between labs never happen: one DHCP server hands out addresses.
//
// Isolation: NAT network objects of different instances (user + lab) and different NAT networks of one
// instance are separated at the bridge level (nftables rules in the bridge family), see the network package.
package natcloud

// Bridge - the name of the shared NAT cloud bridge on the host. Does not match the nrn<hex> format of network bridges.
const Bridge = "nrnat"

// Subnet - the cloud's address space. The gateway is the bridge address on the host.
const (
	Subnet   = "10.77.0.0/16"
	Gateway  = "10.77.0.1"
	PrefixOK = 16
)

// DHCP pool: the whole subnet range except the gateway address and broadcast addresses.
const (
	DHCPStart = "10.77.0.10"
	DHCPEnd   = "10.77.255.250"
)

// Fields returns the cloud's address fields in the form in which they are stored and shown in the topology.
func Fields() (subnet, gateway, dhcpStart, dhcpEnd string) {
	return Subnet, Gateway, DHCPStart, DHCPEnd
}
