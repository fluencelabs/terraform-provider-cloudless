package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Pricing. The numbers here are not vodopad's — they are a stable arithmetic
// the tests can assert against, so a broken request body shows up as a wrong
// price rather than as a plausible one.
const (
	priceVMHourly      = 0.05
	priceGbHourly      = 0.0001
	pricePublicIPHour  = 0.004
	priceReplicaFactor = 2
)

func (s *Server) wireEstimatesOnce() { s.estimatesWiring.Do(s.wireEstimates) }

func (s *Server) wireEstimates() {
	s.mux.HandleFunc("/v3/vm-estimates", s.handleVMEstimate)
}

// handleVMEstimate serves POST /v3/vm-estimates: it prices a complete
// specification without creating anything.
func (s *Server) handleVMEstimate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.notFound(w, r)
		return
	}
	var body struct {
		BootDisk   *estimateDisk  `json:"bootDisk"`
		DataDisks  []estimateDisk `json:"dataDisks"`
		Interfaces []struct {
			Kind string `json:"kind"`
		} `json:"interfaces"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	total := priceVMHourly
	incremental := priceVMHourly
	if body.BootDisk != nil {
		total += body.BootDisk.price()
		if body.BootDisk.StorageID == "" {
			incremental += body.BootDisk.price()
		}
	}
	for _, d := range body.DataDisks {
		total += d.price()
		if d.StorageID == "" {
			incremental += d.price()
		}
	}
	for _, i := range body.Interfaces {
		if i.Kind == interfaceKindPublic {
			total += pricePublicIPHour
			incremental += pricePublicIPHour
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"hourlyTotal":       fmt.Sprintf("%.4f", total),
		"hourlyIncremental": fmt.Sprintf("%.4f", incremental),
		"currency":          "USD",
		"calculatedAt":      "2026-01-01T00:00:00Z",
	})
}

type estimateDisk struct {
	StorageID  string `json:"storageId"`
	VolumeGb   uint64 `json:"volumeGb"`
	Replicated bool   `json:"replicated"`
}

// price charges an existing disk too: hourlyTotal counts everything the VM
// would run with, and the caller decides what is incremental.
func (d estimateDisk) price() float64 {
	gb := float64(d.VolumeGb)
	if d.StorageID != "" {
		gb = draftBootDiskGb
	}
	p := gb * priceGbHourly
	if d.Replicated {
		p *= priceReplicaFactor
	}
	return p
}
