package ovh

import (
	"strings"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

var regionCatalog = []schemas.ProviderRegion{
	{Id: "bhs", Name: "Beauharnois, Canada", Country: "CA", City: ptr("Beauharnois"), Available: true},
	{Id: "gra", Name: "Gravelines, France", Country: "FR", City: ptr("Gravelines"), Available: true},
	{Id: "sbg", Name: "Strasbourg, France", Country: "FR", City: ptr("Strasbourg"), Available: true},
	{Id: "waw", Name: "Warsaw, Poland", Country: "PL", City: ptr("Warsaw"), Available: true},
	{Id: "uk", Name: "London, United Kingdom", Country: "GB", City: ptr("London"), Available: true},
	{Id: "de", Name: "Limburg, Germany", Country: "DE", City: ptr("Limburg"), Available: true},
}

type ovhSize struct {
	id           string
	flavorID     string
	cpuVCores    int
	ramGB        int
	storageGB    int
	storageType  schemas.ProviderStorageType
	bandwidthTB  int
	monthlyUSD   float32
	storageUSD   float32
	bandwidthUSD float32
	tier         schemas.ProviderSizeTier
	recommended  bool
}

var sizeCatalog = []ovhSize{
	{
		id: "vps-3", flavorID: "vps-3",
		cpuVCores: 8, ramGB: 32, storageGB: 320, storageType: schemas.NVMe,
		bandwidthTB: 32, monthlyUSD: 24, storageUSD: 2, bandwidthUSD: 3,
		tier: schemas.Minimum,
	},
	{
		id: "vps-4", flavorID: "vps-4",
		cpuVCores: 12, ramGB: 48, storageGB: 480, storageType: schemas.NVMe,
		bandwidthTB: 32, monthlyUSD: 32, storageUSD: 3, bandwidthUSD: 3,
		tier: schemas.Workable,
	},
	{
		id: "vps-5", flavorID: "vps-5",
		cpuVCores: 16, ramGB: 64, storageGB: 640, storageType: schemas.NVMe,
		bandwidthTB: 32, monthlyUSD: 40, storageUSD: 4, bandwidthUSD: 4,
		tier: schemas.Recommended, recommended: true,
	},
}

var regionNameByID = map[string]string{
	"bhs": "BHS5",
	"gra": "GRA11",
	"sbg": "SBG5",
	"waw": "WAW1",
	"uk":  "UK1",
	"de":  "DE1",
}

func (s ovhSize) toSchema() schemas.ProviderSize {
	return schemas.ProviderSize{
		Id:          s.id,
		CpuVCores:   s.cpuVCores,
		RamGB:       s.ramGB,
		StorageGB:   s.storageGB,
		StorageType: s.storageType,
		BandwidthTB: s.bandwidthTB,
		MonthlyUSD:  s.monthlyUSD,
		Tier:        s.tier,
		Recommended: ptr(s.recommended),
	}
}

func knownRegion(regionID string) bool {
	_, ok := regionNameByID[strings.TrimSpace(regionID)]
	return ok
}

func regionAPIName(regionID string) string {
	if name, ok := regionNameByID[strings.TrimSpace(regionID)]; ok {
		return name
	}
	return regionID
}

func regionIDForAPIName(name string) string {
	name = strings.TrimSpace(name)
	for id, candidate := range regionNameByID {
		if strings.EqualFold(candidate, name) {
			return id
		}
	}
	if knownRegion(name) {
		return name
	}
	return "bhs"
}

func lookupSize(id string) (ovhSize, bool) {
	id = strings.TrimSpace(id)
	for _, size := range sizeCatalog {
		if size.id == id || size.flavorID == id {
			return size, true
		}
	}
	return ovhSize{}, false
}

func sizeIDForFlavor(flavorID string) string {
	if size, ok := lookupSize(flavorID); ok {
		return size.id
	}
	return flavorID
}

func ptr[T any](v T) *T {
	return &v
}
