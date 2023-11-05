package melodix

import (
	"app/internal/db"
)

// MelodixHistory manages the history of songs played in the application.
type MelodixHistory struct{}

// HistoryTrackInfo contains information about a history entry and its associated track.
type HistoryTrackInfo struct {
	History db.History
	Track   db.Track
}

// IMelodixHistory defines the interface for managing the application's play history.
type IMelodixHistory interface {
	AddTrackToHistory(guildID string, song *Song) error
	AddPlaybackAllStats(guildID, ytid string, duration float64) error
	AddPlaybackCountStats(guildID, ytid string) error
	AddPlaybackDurationStats(guildID, ytid string, duration float64) error
	GetHistory(guildID string, sortBy string) ([]HistoryTrackInfo, error)
	GetTrackFromHistory(guildID string, trackID uint) (db.Track, error)
}

// NewHistory creates a new MelodixHistory instance.
func NewHistory() IMelodixHistory {
	return &MelodixHistory{}
}

// AddTrackToHistory adds a song to the application's play history.
func (mp *MelodixHistory) AddTrackToHistory(guildID string, song *Song) error {
	var track *db.Track

	existingTrack, err := db.GetTrackByYTID(song.ID)
	if err != nil {
		newTrack := &db.Track{
			YTID: song.ID,
			Name: song.Name,
			URL:  song.UserURL,
		}

		if err := db.CreateTrack(newTrack); err != nil {
			return err
		}

		existingTrack, _ = db.GetTrackByYTID(song.ID)
	}

	if existingTrack == nil {
		newTrack := &db.Track{
			YTID: song.ID,
			Name: song.Name,
			URL:  song.UserURL,
		}
		track = newTrack
	} else {
		track = existingTrack
	}

	exists, err := db.DoesHistoryExistForGuild(track.ID, guildID)
	if err != nil {
		return err
	}

	if !exists {
		history := db.History{
			GuildID: guildID,
			TrackID: track.ID,
		}
		return db.CreateHistory(&history)
	}

	return nil
}

// AddPlaybackStats updates all playback statistics (duration and count) for a track.
func (mp *MelodixHistory) AddPlaybackAllStats(guildID, ytid string, duration float64) error {

	existingTrackRecord, err := db.GetTrackByYTID(ytid)
	if err != nil {
		return err
	}

	existingHistoryRecord, err := db.GetHistoryByTrackIDAndGuildID(existingTrackRecord.ID, guildID)
	if err != nil {
		return err
	}

	newPlayCount := existingHistoryRecord.PlayCount + 1
	newDuration := existingHistoryRecord.Duration + duration

	return db.UpdateTrackStatsForGuild(existingTrackRecord.ID, guildID, newPlayCount, newDuration)
}

// AddPlaybackCountStats updates playback count statistics for a track.
func (mp *MelodixHistory) AddPlaybackCountStats(guildID, ytid string) error {

	existingTrackRecord, err := db.GetTrackByYTID(ytid)
	if err != nil {
		return err
	}

	existingHistoryRecord, err := db.GetHistoryByTrackIDAndGuildID(existingTrackRecord.ID, guildID)
	if err != nil {
		return err
	}

	newPlayCount := existingHistoryRecord.PlayCount + 1
	newDuration := existingHistoryRecord.Duration

	return db.UpdateTrackStatsForGuild(existingTrackRecord.ID, guildID, newPlayCount, newDuration)
}

// AddPlaybackDurationStats updates playback duration statistics for a track.
func (mp *MelodixHistory) AddPlaybackDurationStats(guildID, ytid string, duration float64) error {

	existingTrackRecord, err := db.GetTrackByYTID(ytid)
	if err != nil {
		return err
	}

	existingHistoryRecord, err := db.GetHistoryByTrackIDAndGuildID(existingTrackRecord.ID, guildID)
	if err != nil {
		return err
	}

	newPlayCount := existingHistoryRecord.PlayCount
	newDuration := existingHistoryRecord.Duration + duration

	return db.UpdateTrackStatsForGuild(existingTrackRecord.ID, guildID, newPlayCount, newDuration)
}

// GetHistory retrieves the play history for a guild, sorted by the specified criteria.
func (mp *MelodixHistory) GetHistory(guildID string, sortBy string) ([]HistoryTrackInfo, error) {
	var historyEntries []db.History
	var err error

	if guildID == "" {
		historyEntries, err = db.GetAllHistorySortedBy(sortBy)
		if err != nil {
			return nil, err
		}
	} else {
		historyEntries, err = db.GetGuildHistorySortedBy(guildID, sortBy)
		if err != nil {
			return nil, err
		}
	}

	var historyWithTracks []HistoryTrackInfo

	for _, historyEntry := range historyEntries {

		track, err := db.GetTrackByID(historyEntry.TrackID)
		if err != nil {
			return nil, err
		}

		combinedInfo := HistoryTrackInfo{
			History: historyEntry,
			Track:   *track,
		}

		historyWithTracks = append(historyWithTracks, combinedInfo)
	}

	return historyWithTracks, nil
}

// GetTrackFromHistory retrieves a track from the play history based on its ID and guild.
func (mp *MelodixHistory) GetTrackFromHistory(guildID string, trackID uint) (db.Track, error) {

	exists, err := db.DoesHistoryExistForGuild(trackID, guildID)
	if err != nil {
		return db.Track{}, err
	}

	if exists {
		track, err := db.GetTrackByID(trackID)
		if err != nil {
			return db.Track{}, err
		}

		return *track, nil
	}

	return db.Track{}, err
}
