package contabo

import (
	"strings"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

var regionCatalog = []schemas.ProviderRegion{
	{Id: "eu-central-1", Name: "European Union (Germany)", Country: "DE", City: ptr("Frankfurt"), Available: true},
	{Id: "us-east-1", Name: "United States East", Country: "US", City: ptr("New York"), Available: true},
	{Id: "us-central-1", Name: "United States Central", Country: "US", City: ptr("St. Louis"), Available: true},
	{Id: "us-west-1", Name: "United States West", Country: "US", City: ptr("Seattle"), Available: true},
	{Id: "ap-southeast-1", Name: "Singapore", Country: "SG", City: ptr("Singapore"), Available: true},
	{Id: "gb-south-1", Name: "United Kingdom", Country: "GB", City: ptr("London"), Available: true},
	{Id: "au-southeast-1", Name: "Australia", Country: "AU", City: ptr("Sydney"), Available: true},
	{Id: "jp-east-1", Name: "Japan", Country: "JP", City: ptr("Tokyo"), Available: true},
	{Id: "in-west-1", Name: "India", Country: "IN", City: ptr("Mumbai"), Available: true},
}

type contaboSize struct {
	id           string
	productID    string
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

var sizeCatalog = []contaboSize{
	{
		id: "cloud-vps-30", productID: "V97",
		cpuVCores: 8, ramGB: 32, storageGB: 200, storageType: schemas.NVMe,
		bandwidthTB: 32, monthlyUSD: 32, storageUSD: 2, bandwidthUSD: 4,
		tier: schemas.Minimum,
	},
	{
		id: "cloud-vps-40", productID: "V100",
		cpuVCores: 12, ramGB: 48, storageGB: 250, storageType: schemas.NVMe,
		bandwidthTB: 32, monthlyUSD: 44, storageUSD: 2, bandwidthUSD: 4,
		tier: schemas.Workable,
	},
	{
		id: "cloud-vps-50", productID: "V103",
		cpuVCores: 16, ramGB: 64, storageGB: 300, storageType: schemas.NVMe,
		bandwidthTB: 32, monthlyUSD: 56, storageUSD: 2, bandwidthUSD: 4,
		tier: schemas.Recommended, recommended: true,
	},
}

var regionSlugByID = map[string]string{
	"eu-central-1":   "EU",
	"us-east-1":      "US-east",
	"us-central-1":   "US-central",
	"us-west-1":      "US-west",
	"ap-southeast-1": "SIN",
	"gb-south-1":     "UK",
	"au-southeast-1": "AUS",
	"jp-east-1":      "JPN",
	"in-west-1":      "IND",
}

func (s contaboSize) toSchema() schemas.ProviderSize {
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

func (s contaboSize) regionSurchargeUSD(regionID string) float32 {
	if strings.HasPrefix(regionID, "us-") {
		return 10
	}
	return 0
}

func knownRegion(regionID string) bool {
	_, ok := regionSlugByID[strings.TrimSpace(regionID)]
	return ok
}

func regionSlug(regionID string) string {
	if slug, ok := regionSlugByID[strings.TrimSpace(regionID)]; ok {
		return slug
	}
	return regionID
}

func regionIDForSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	for id, candidate := range regionSlugByID {
		if strings.EqualFold(candidate, slug) {
			return id
		}
	}
	if knownRegion(slug) {
		return slug
	}
	return "eu-central-1"
}

func lookupSize(id string) (contaboSize, bool) {
	id = strings.TrimSpace(id)
	for _, size := range sizeCatalog {
		if size.id == id || size.productID == id {
			return size, true
		}
	}
	return contaboSize{}, false
}

func sizeIDForProduct(productID string) string {
	if size, ok := lookupSize(productID); ok {
		return size.id
	}
	return productID
}

func ptr[T any](v T) *T {
	return &v
}
