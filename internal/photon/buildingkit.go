package photon

import (
	"encoding/xml"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type bkComposablePart struct {
	Name string `xml:"m_sPartName"`
	Fbxs []struct {
		Materials []struct{} `xml:"m_aMaterials>ComposableMaterial"`
	} `xml:"m_aFbxs>ComposableFBX"`
}

type bkComposableCharacter struct {
	BkID  uint32             `xml:"bkID"`
	Parts []bkComposablePart `xml:"m_aParts>ComposablePart"`
}

var (
	buildingKitsOnce sync.Once
	// bkID -> per-part -> per-fbx-variant material count
	buildingKitMatCounts map[uint32][][]int
)

func loadBuildingKits() {
	buildingKitsOnce.Do(func() {
		buildingKitMatCounts = map[uint32][][]int{}
		matches, err := filepath.Glob("data/buildingkits/BKT_PC_*.xml")
		if err != nil || len(matches) == 0 {
			log.Printf("[AVATAR] no data/buildingkits/BKT_PC_*.xml found: %v", err)
			return
		}
		for _, path := range matches {
			raw, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[AVATAR] %s not loaded: %v", path, err)
				continue
			}
			var bk bkComposableCharacter
			if err := xml.Unmarshal(raw, &bk); err != nil {
				log.Printf("[AVATAR] %s parse error: %v", path, err)
				continue
			}
			parts := make([][]int, len(bk.Parts))
			for i, p := range bk.Parts {
				counts := make([]int, len(p.Fbxs))
				for j, fbx := range p.Fbxs {
					counts[j] = len(fbx.Materials)
				}
				parts[i] = counts
			}
			buildingKitMatCounts[bk.BkID] = parts
		}
		log.Printf("[AVATAR] loaded BuildingKit material tables for %d classes", len(buildingKitMatCounts))
	})
}

func clampAvatarMaterials(avatarID uint32, fbxs, mats []byte) []byte {
	loadBuildingKits()
	parts, ok := buildingKitMatCounts[avatarID]
	if !ok {
		return mats
	}
	for i := 0; i < len(mats) && i < len(fbxs) && i < len(parts); i++ {
		fbxIdx := int(fbxs[i])
		if fbxIdx < 0 || fbxIdx >= len(parts[i]) {
			continue // out-of-range FBX is a separate (Fbx, not Material) error, not ours to fix here
		}
		matCount := parts[i][fbxIdx]
		if matCount > 0 && int(mats[i]) >= matCount {
			mats[i] = 0
		}
	}
	return mats
}

func clampAvatarMaterialsCSV(avatarID uint32, avatarPartsCSV, avatarMatsCSV string) string {
	fbxs := parseByteCSV(avatarPartsCSV)
	mats := parseByteCSV(avatarMatsCSV)
	for len(mats) < len(fbxs) {
		mats = append(mats, 0)
	}
	return bytesToCSV(clampAvatarMaterials(avatarID, fbxs, mats))
}

func bytesToCSV(b []byte) string {
	out := make([]byte, 0, len(b)*2)
	for i, v := range b {
		if i > 0 {
			out = append(out, ',')
		}
		out = strconv.AppendInt(out, int64(v), 10)
	}
	return string(out)
}
