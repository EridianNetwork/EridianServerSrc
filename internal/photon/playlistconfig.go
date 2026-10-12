package photon

import (
	"encoding/xml"
	"log"
	"os"
	"sync"
)

type xmlPlaylistScene struct {
	Scenes []string `xml:"m_scene"`
}

type xmlGradeScore struct {
	Grade string `xml:"m_grade"`
	Score int    `xml:"m_score"`
}

type xmlPlaylist struct {
	ID                    int32            `xml:"m_id"`
	Name                  string           `xml:"m_name"`
	GameMode              string           `xml:"m_gameMode"`
	MaxPlayerCount        int              `xml:"m_maxPlayerCount"`
	MinPlayerCountToStart int              `xml:"m_minPlayerCountToStart"`
	SceneToPlay           xmlPlaylistScene `xml:"m_sceneToPlay"`
	BossName              string           `xml:"m_bossName"`
	NormalLevelMin        int              `xml:"m_normal_LevelMin"`
	NormalLevelMax        int              `xml:"m_normal_LevelMax"`
	HardLevelMin          int              `xml:"m_hard_LevelMin"`
	HardLevelMax          int              `xml:"m_hard_LevelMax"`
	HellLevelMin          int              `xml:"m_hell_LevelMin"`
	HellLevelMax          int              `xml:"m_hell_LevelMax"`
	NormalExp             int              `xml:"m_normal_Exp"`
	HardExp               int              `xml:"m_hard_Exp"`
	HellExp               int              `xml:"m_hell_Exp"`
	NormalTargetScore     []xmlGradeScore  `xml:"m_normal_targetScore>m_gradeScore"`
	HardTargetScore       []xmlGradeScore  `xml:"m_hard_targetScore>m_gradeScore"`
	HellTargetScore       []xmlGradeScore  `xml:"m_hell_targetScore>m_gradeScore"`
}

type xmlPlaylistSetup struct {
	Playlists []xmlPlaylist `xml:"m_playlists>m_playlist"`
}

var (
	playlistConfigOnce sync.Once
	playlistByID       map[int32]*xmlPlaylist
)

func loadPlaylistConfig() {
	playlistConfigOnce.Do(func() {
		playlistByID = map[int32]*xmlPlaylist{}
		raw, err := os.ReadFile("data/playlists/Playlist.xml")
		if err != nil {
			log.Printf("[PLAYLIST] data/playlists/Playlist.xml not found: %v", err)
			return
		}
		var setup xmlPlaylistSetup
		if err := xml.Unmarshal(raw, &setup); err != nil {
			log.Printf("[PLAYLIST] bad Playlist.xml: %v", err)
			return
		}
		for i := range setup.Playlists {
			p := &setup.Playlists[i]
			playlistByID[p.ID] = p
		}
		log.Printf("[PLAYLIST] loaded %d playlists from data/playlists/Playlist.xml", len(playlistByID))
	})
}

func playlistInfo(id int32) *xmlPlaylist {
	loadPlaylistConfig()
	return playlistByID[id]
}

func playlistFirstScene(id int32) (string, bool) {
	p := playlistInfo(id)
	if p == nil || len(p.SceneToPlay.Scenes) == 0 {
		return "", false
	}
	return p.SceneToPlay.Scenes[0], true
}

func playlistSceneAt(id, idx int32) (string, bool) {
	p := playlistInfo(id)
	if p == nil || idx < 0 || int(idx) >= len(p.SceneToPlay.Scenes) {
		return "", false
	}
	return p.SceneToPlay.Scenes[idx], true
}

func playlistLevelRange(id int32, difficulty int32) (min, max int, ok bool) {
	p := playlistInfo(id)
	if p == nil {
		return 0, 0, false
	}
	switch difficulty {
	case 0:
		return p.NormalLevelMin, p.NormalLevelMax, true
	case 1:
		return p.HardLevelMin, p.HardLevelMax, true
	case 2:
		return p.HellLevelMin, p.HellLevelMax, true
	}
	return 0, 0, false
}

func playlistGrade(id, difficulty, score int32) string {
	p := playlistInfo(id)
	if p == nil {
		return "F"
	}
	var table []xmlGradeScore
	switch difficulty {
	case 0:
		table = p.NormalTargetScore
	case 1:
		table = p.HardTargetScore
	case 2:
		table = p.HellTargetScore
	}
	for _, g := range table {
		if score >= int32(g.Score) {
			return g.Grade
		}
	}
	return "F"
}
