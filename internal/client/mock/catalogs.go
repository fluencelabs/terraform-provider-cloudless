package mock

import "net/http"

// The read-only catalogs a configuration picks from: VM presets and OS
// images. Both come wrapped as {"items": […]}.

// Catalog ids the unit tests build configurations around.
const (
	catalogConfigSmallID = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcfcf"
	catalogConfigLargeID = "cfcfcfcf-cfcf-4cfc-8cfc-cfcfcfcfcf22"
	catalogImageUbuntuID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	catalogImageDebianID = "cccccccc-cccc-4ccc-8ccc-cccccccccc22"
)

// The two presets the catalog offers, small and large.
const (
	presetSmallVCPU  = 2
	presetSmallRAMGb = 4
	presetLargeVCPU  = 8
	presetLargeRAMGb = 16
)

func (s *Server) wireCatalogsOnce() { s.catalogsWiring.Do(s.wireCatalogs) }

func (s *Server) wireCatalogs() {
	presets := []map[string]any{
		vmConfigWire(vmPreset{
			id: catalogConfigSmallID, slug: "cpu-regular-2vcpu-4gb", name: "CPU 2 / RAM 4GB", vcpu: presetSmallVCPU, ramGb: presetSmallRAMGb,
		}),
		vmConfigWire(vmPreset{
			id: catalogConfigLargeID, slug: "cpu-regular-8vcpu-16gb", name: "CPU 8 / RAM 16GB", vcpu: presetLargeVCPU, ramGb: presetLargeRAMGb,
		}),
	}
	s.mux.HandleFunc("/v1/configurations/virtual_machines", func(w http.ResponseWriter, _ *http.Request) {
		s.writeJSON(w, http.StatusOK, map[string]any{"items": presets})
	})

	s.mux.HandleFunc("/v1/storages/default_images", func(w http.ResponseWriter, _ *http.Request) {
		s.writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]any{
			imageWire(catalogImageUbuntuID, "ubuntu-24-04-x64", "Ubuntu 24.04", "Ubuntu", "ubuntu"),
			imageWire(catalogImageDebianID, "debian-12-x64", "Debian 12", "Debian", "debian"),
		}})
	})
}

type vmPreset struct {
	id, slug, name string
	vcpu, ramGb    int
}

func vmConfigWire(p vmPreset) map[string]any {
	return map[string]any{
		"id":          p.id,
		"slug":        p.slug,
		"name":        p.name,
		"vcpu":        p.vcpu,
		"ramGb":       p.ramGb,
		"dedicated":   false,
		"cpuFamilies": []string{"AMD_ZEN3"},
		"preset":      "general",
		"tags":        []string{},
		"description": p.name,
	}
}

func imageWire(id, slug, name, distribution, username string) map[string]any {
	return map[string]any{
		"id":           id,
		"slug":         slug,
		"name":         name,
		"distribution": distribution,
		"downloadUrl":  "https://example.com/" + slug + ".qcow2",
		"iconUrl":      "https://example.com/" + slug + ".svg",
		"username":     username,
		"isDefault":    true,
		"bootMode":     "EFI",
		"createdAt":    "2026-01-01T00:00:00Z",
		"updatedAt":    "2026-01-01T00:00:00Z",
	}
}
