package main

import (
	"fmt"
	"log/slog"
	"strings"

	"fyne.io/fyne/v2"
	"github.com/slackhq/nebula/config"
	"go.yaml.in/yaml/v3"
)

// viaGroup is every tun.unsafe_routes entry that shares a single "via"
// target, kept together so the tray can enable or disable them as a unit.
type viaGroup struct {
	// key is the canonical, human readable form of the entry's "via" - it
	// doubles as the menu label and the enabledVias key.
	key string
	// entries holds the original, unmodified config entries so re-injecting
	// them preserves mtu/metric/install without a lossy round trip.
	entries []any
	// routes are the entries' "route" values, shown greyed out under the key.
	routes []string
}

// route state. All of it is guarded by togglemtx, alongside the nebula
// start/stop state it has to stay consistent with.
var (
	// baseYAML is the loaded config with tun.unsafe_routes stripped out. It
	// is the base every rendered config is built back up from.
	baseYAML string

	// viaGroups is the stripped out tun.unsafe_routes, grouped by via and in
	// first-seen order so the menu is stable across refreshes.
	viaGroups []viaGroup

	// enabledVias tracks which via keys the user has switched on. It
	// deliberately outlives a stop/start cycle so toggles made while nebula
	// is down are applied on the next start.
	enabledVias = map[string]bool{}
)

// loadNebulaConfig loads the config from disk, splits the unsafe routes out
// of it, and hands back a config.C holding only the currently enabled ones.
func loadNebulaConfig() (*config.C, error) {
	c := config.NewC(l)
	if err := c.Load(*configPath); err != nil {
		return nil, err
	}

	if err := splitUnsafeRoutes(c); err != nil {
		return nil, err
	}

	raw, err := renderConfig()
	if err != nil {
		return nil, err
	}

	if err := c.LoadString(raw); err != nil {
		return nil, err
	}

	return c, nil
}

// splitUnsafeRoutes moves tun.unsafe_routes out of c.Settings and into
// viaGroups, leaving the remainder as baseYAML.
func splitUnsafeRoutes(c *config.C) error {
	var raw []any

	if tun, ok := c.Settings["tun"].(map[string]any); ok {
		if ur, ok := tun["unsafe_routes"].([]any); ok {
			raw = ur
		}
		delete(tun, "unsafe_routes")
	}

	b, err := yaml.Marshal(c.Settings)
	if err != nil {
		return fmt.Errorf("rendering config without unsafe routes: %w", err)
	}

	baseYAML = string(b)
	viaGroups = groupByVia(raw)
	return nil
}

// groupByVia collects unsafe route entries under their via, preserving the
// order the vias first appear in the config.
func groupByVia(entries []any) []viaGroup {
	var groups []viaGroup
	index := map[string]int{}

	for _, e := range entries {
		m, ok := e.(map[string]any)
		if !ok {
			l.Warn("skipping unsafe route entry that is not a map", slog.Any("entry", e))
			continue
		}

		key := viaKey(m["via"])
		i, ok := index[key]
		if !ok {
			index[key] = len(groups)
			i = len(groups)
			groups = append(groups, viaGroup{key: key})
		}

		groups[i].entries = append(groups[i].entries, e)
		groups[i].routes = append(groups[i].routes, fmt.Sprint(m["route"]))
	}

	return groups
}

// viaKey canonicalizes a "via" value into a single string. Since nebula 1.11
// a via is either a plain address or a list of {gateway, weight} maps, so the
// multi-gateway form is flattened to its comma separated gateways.
func viaKey(v any) string {
	switch via := v.(type) {
	case string:
		return via

	case []any:
		gateways := make([]string, 0, len(via))
		for _, g := range via {
			if m, ok := g.(map[string]any); ok {
				gateways = append(gateways, fmt.Sprint(m["gateway"]))
				continue
			}
			gateways = append(gateways, fmt.Sprint(g))
		}
		return strings.Join(gateways, ", ")

	default:
		return fmt.Sprint(v)
	}
}

// renderConfig rebuilds the full config from baseYAML plus the enabled via
// groups. Unmarshaling baseYAML each time gives a fresh map, which matters:
// config.C keeps only a shallow copy of the old settings for change
// detection, so mutating the live map in place would make HasChanged - and
// therefore the tun route reload - miss the change entirely.
func renderConfig() (string, error) {
	var m map[string]any
	if err := yaml.Unmarshal([]byte(baseYAML), &m); err != nil {
		return "", fmt.Errorf("reparsing base config: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}

	var entries []any
	for _, g := range viaGroups {
		if enabledVias[g.key] {
			entries = append(entries, g.entries...)
		}
	}

	if len(entries) > 0 {
		tun, ok := m["tun"].(map[string]any)
		if !ok {
			tun = map[string]any{}
			m["tun"] = tun
		}
		tun["unsafe_routes"] = entries
	}

	b, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("rendering config: %w", err)
	}

	return string(b), nil
}

// refreshViaGroups reloads just the route list from disk, so the tray menu is
// populated before nebula has ever been started.
func refreshViaGroups() error {
	c := config.NewC(l)
	if err := c.Load(*configPath); err != nil {
		return err
	}
	return splitUnsafeRoutes(c)
}

// toggleVia flips a via group on or off. When nebula is running the change is
// pushed live via ReloadConfigString, which the tun reload callback turns into
// route table adds and removes; otherwise it just waits for the next start.
func toggleVia(key string) {
	togglemtx.Lock()
	defer togglemtx.Unlock()

	enabledVias[key] = !enabledVias[key]
	l.Info("toggling unsafe routes", slog.String("via", key), slog.Bool("enabled", enabledVias[key]))

	if state == StateStarted && cfg != nil {
		if err := applyRoutes(); err != nil {
			// Roll back so the menu keeps matching what nebula is running.
			enabledVias[key] = !enabledVias[key]
			l.Error("failed to apply route change", slog.String("via", key), slog.String("error", err.Error()))
		}
	}

	updateSystrayMenu()
}

// applyRoutes renders the current selection and reloads nebula with it.
func applyRoutes() error {
	raw, err := renderConfig()
	if err != nil {
		return err
	}
	return cfg.ReloadConfigString(raw)
}

// routesMenuItem builds the "Routes" submenu: one checkable item per via,
// with that via's routes listed beneath it, disabled.
func routesMenuItem() *fyne.MenuItem {
	items := make([]*fyne.MenuItem, 0, len(viaGroups)*3)

	for i, g := range viaGroups {
		if i > 0 {
			items = append(items, fyne.NewMenuItemSeparator())
		}

		key := g.key
		via := fyne.NewMenuItem("via "+key, func() { toggleVia(key) })
		via.Checked = enabledVias[key]
		items = append(items, via)

		for _, r := range g.routes {
			route := fyne.NewMenuItem("    "+r, nil)
			route.Disabled = true
			items = append(items, route)
		}
	}

	routes := fyne.NewMenuItem("Routes", nil)
	routes.ChildMenu = fyne.NewMenu("Routes", items...)
	return routes
}
