package models

import "time"

// KVM network settings and event log (/api/network/*, /api/events): the
// device menu's Network settings and Event log, for boards without a
// screen.

// NetIPConfig is DHCP or a static address.
type NetIPConfig struct {
	Mode    string `json:"mode"` // "dhcp" or "static"
	IP      string `json:"ip,omitempty"`
	Netmask string `json:"netmask,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	DNS     string `json:"dns,omitempty"`
}

type EthernetStatus struct {
	Available bool        `json:"available"`
	Interface string      `json:"interface,omitempty"`
	Connected bool        `json:"connected"`
	IP        string      `json:"ip,omitempty"`
	MAC       string      `json:"mac,omitempty"`
	Gateway   string      `json:"gateway,omitempty"`
	Speed     string      `json:"speed,omitempty"`
	Config    NetIPConfig `json:"config"`
}

type WiFiStatus struct {
	Available  bool        `json:"available"`
	Interface  string      `json:"interface,omitempty"`
	Enabled    bool        `json:"enabled"`
	Connected  bool        `json:"connected"`
	Connecting bool        `json:"connecting"`
	SSID       string      `json:"ssid,omitempty"`
	Signal     int         `json:"signal"`
	IP         string      `json:"ip,omitempty"`
	LastError  string      `json:"last_error,omitempty"`
	Config     NetIPConfig `json:"config"`
}

type NetworkStatus struct {
	Ethernet EthernetStatus `json:"ethernet"`
	WiFi     WiFiStatus     `json:"wifi"`
}

type WiFiNetwork struct {
	SSID      string `json:"ssid"`
	Signal    int    `json:"signal"`
	Security  string `json:"security"`
	Connected bool   `json:"connected"`
}

// KVMEvent is one entry of the KVM's event log.
type KVMEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	Type       string    `json:"type"`
	ShortDesc  string    `json:"short_desc"`
	DetailDesc string    `json:"detail_desc"`
	ExtraData  string    `json:"extra_data"`
}

// KVM power & performance (/api/settings/power).
type PowerStep struct {
	Value int    `json:"value"`
	Label string `json:"label"`
}

type PowerRow struct {
	Key     string      `json:"key"`
	Label   string      `json:"label"`
	Steps   []PowerStep `json:"steps"`
	Value   int         `json:"value"`
	Default int         `json:"default"`
}

type PowerSettings struct {
	Available bool       `json:"available"`
	Rows      []PowerRow `json:"rows"`
}

// KVM firmware updates (/api/settings/update).
type UpdateStatus struct {
	Version  string `json:"version"`
	Checking bool   `json:"checking"`
	Status   string `json:"status,omitempty"`
	Message  string `json:"message,omitempty"`
	At       string `json:"at,omitempty"`
}

type SnapshotTiming struct {
	QuietPeriodSec         int `json:"quiet_period_sec"`
	MinSnapshotGapSec      int `json:"min_snapshot_gap_sec"`
	MaxSnapshotIntervalSec int `json:"max_snapshot_interval_sec"`
}

// KVM SD card (/api/settings/sdcard).
type SDCardStatus struct {
	Present         bool           `json:"present"`
	Mounted         bool           `json:"mounted"`
	FileSystem      string         `json:"filesystem,omitempty"`
	TotalBytes      uint64         `json:"total_bytes"`
	UsedBytes       uint64         `json:"used_bytes"`
	FreeBytes       uint64         `json:"free_bytes"`
	FormatAllowed   bool           `json:"format_allowed"`
	FormatBlockedBy string         `json:"format_blocked_by,omitempty"`
	Formatting      bool           `json:"formatting"`
	FormatStep      string         `json:"format_step,omitempty"`
	FormatError     string         `json:"format_error,omitempty"`
	Snapshots       SnapshotTiming `json:"snapshots"`
}
