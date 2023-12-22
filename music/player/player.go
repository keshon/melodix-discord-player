// Package melodix provides audio playback management for a Discord bot.
package player

import (
	"io"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gookit/slog"

	"github.com/keshon/melodix-discord-player/internal/config"
	"github.com/keshon/melodix-discord-player/music/history"
	"github.com/keshon/melodix-discord-player/music/pkg/dca"
	"github.com/keshon/melodix-discord-player/music/utils"
)

type Thumbnail struct {
	URL    string
	Width  uint
	Height uint
}

// SongSource represents the source type of the media.
type SongSource int32

const (
	SourceYouTube SongSource = iota
	SourceStream
)

// String returns the string representation of the SongSource.
func (source SongSource) String() string {
	sources := map[SongSource]string{
		SourceYouTube: "YouTube",
		SourceStream:  "Stream",
	}

	return sources[source]
}

// Song represents a media item with relevant information.
type Song struct {
	Title       string        // Title of the song
	UserURL     string        // URL provided by the user
	DownloadURL string        // URL for downloading the song
	Thumbnail   Thumbnail     // Thumbnail image for the song
	Duration    time.Duration // Duration of the song
	ID          string        // Unique ID for the song
	Source      SongSource    // Source type of the song
}

// PlaybackStatus represents the playback status of the Player.
type PlaybackStatus int32

const (
	StatusResting PlaybackStatus = iota
	StatusPlaying
	StatusPaused
	StatusError
)

// String returns the string representation of the PlaybackStatus.
func (status PlaybackStatus) String() string {
	statuses := map[PlaybackStatus]string{
		StatusResting: "Resting",
		StatusPlaying: "Playing",
		StatusPaused:  "Paused",
		StatusError:   "Error",
	}

	return statuses[status]
}

func (status PlaybackStatus) StringEmoji() string {
	statuses := map[PlaybackStatus]string{
		StatusResting: "⏹️",
		StatusPlaying: "▶️",
		StatusPaused:  "⏸",
		StatusError:   "⚠️",
	}

	return statuses[status]
}

// Player manages audio playback and song queue.
type Player struct {
	sync.Mutex
	VoiceConnection  *discordgo.VoiceConnection
	StreamingSession *dca.StreamingSession
	EncodingSession  *dca.EncodeSession
	SongQueue        []*Song
	CurrentSong      *Song
	CurrentStatus    PlaybackStatus
	SkipInterrupt    chan bool
}

// IPlayer defines the interface for managing audio playback and song queue.
type IPlayer interface {
	Play(startAt int, song *Song)
	Skip()
	Enqueue(song *Song)
	Dequeue() *Song
	ClearQueue()
	Stop()
	Pause()
	Unpause()
	GetCurrentStatus() PlaybackStatus
	SetCurrentStatus(status PlaybackStatus)
	GetSongQueue() []*Song
	GetVoiceConnection() *discordgo.VoiceConnection
	SetVoiceConnection(voiceConnection *discordgo.VoiceConnection)
	GetStreamingSession() *dca.StreamingSession
	GetCurrentSong() *Song
}

// NewPlayer creates a new Player instance.
func NewPlayer(guildID string) IPlayer {
	return &Player{
		VoiceConnection:  nil,
		SkipInterrupt:    make(chan bool, 1),
		StreamingSession: nil,
		EncodingSession:  nil,
		SongQueue:        make([]*Song, 0),
		CurrentSong:      nil,
		CurrentStatus:    StatusResting,
	}
}

// Skip skips to the next song in the queue.
func (p *Player) Skip() {
	slog.Info("Skipping to next song")

	switch p.CurrentStatus {
	case StatusPlaying, StatusPaused:
		if p.VoiceConnection == nil || p.CurrentSong == nil {
			return
		}

		p.CurrentStatus = StatusResting

		if len(p.SkipInterrupt) == 0 {
			history := history.NewHistory()
			history.AddPlaybackCountStats(p.VoiceConnection.GuildID, p.CurrentSong.ID)

			p.SkipInterrupt <- true
			p.Play(0, nil)
		}
	case StatusResting:
		if p.CurrentSong != nil {
			if len(p.SkipInterrupt) == 0 {
				history := history.NewHistory()
				history.AddPlaybackCountStats(p.VoiceConnection.GuildID, p.CurrentSong.ID)

				p.SkipInterrupt <- true
				p.Play(0, nil)
				p.CurrentStatus = StatusPlaying
			}
		} else {
			if len(p.SkipInterrupt) == 0 {
				p.SkipInterrupt <- true
				p.Play(0, nil)
				p.CurrentStatus = StatusPlaying
			}
		}
	}

}

// Enqueue adds a song to the queue.
func (p *Player) Enqueue(song *Song) {
	slog.Infof("Enqueuing song to queue: %v", song.Title)

	p.Lock()
	defer p.Unlock()

	p.SongQueue = append(p.SongQueue, song)
}

// Dequeue removes and returns the first song from the queue.
func (p *Player) Dequeue() *Song {
	slog.Info("Dequeuing song and returning it from queue")

	p.Lock()
	defer p.Unlock()

	if len(p.SongQueue) == 0 {
		return nil
	}

	firstSong := p.SongQueue[0]
	p.SongQueue = p.SongQueue[1:]

	return firstSong
}

// ClearQueue clears the song queue.
func (p *Player) ClearQueue() {
	slog.Info("Clearing song queue")

	p.Lock()
	defer p.Unlock()

	if len(p.SongQueue) == 0 {
		return
	}

	p.SongQueue = make([]*Song, 0)
}

// Stop stops audio playback and disconnects from the voice channel.
func (p *Player) Stop() {
	slog.Info("Stopping audio playback and disconnecting from voice channel")

	p.ClearQueue()

	if p.VoiceConnection != nil {
		err := p.VoiceConnection.Speaking(false)
		if err != nil {
			slog.Errorf("Error disconnecting voice connection: %v", err)
		}

		err = p.GetVoiceConnection().Disconnect()
		if err != nil {
			slog.Fatal(err)
		}

		p.SetVoiceConnection(nil)
	}

	if p.StreamingSession != nil {
		p.StreamingSession = nil
	}

	if p.EncodingSession != nil {
		p.EncodingSession.Stop()
		p.EncodingSession.Cleanup()
	}

	if p.CurrentSong != nil {
		p.CurrentSong = nil
	}

	p.CurrentStatus = StatusResting

}

// Pause pauses audio playback.
func (p *Player) Pause() {
	slog.Info("Pausing audio playback")

	if p.VoiceConnection == nil {
		return
	}

	if p.StreamingSession == nil {
		return
	}

	if p.CurrentStatus == StatusPlaying {
		p.StreamingSession.SetPaused(true)
		p.CurrentStatus = StatusPaused
	}
}

// Unpause resumes audio playback.
func (p *Player) Unpause() {
	slog.Info("Resuming playback")

	if p.VoiceConnection == nil {
		return
	}

	if p.StreamingSession != nil {
		if p.CurrentStatus == StatusPaused {
			p.StreamingSession.SetPaused(false)
			p.CurrentStatus = StatusPlaying
		}
	}

	if len(p.GetSongQueue()) > 0 {
		if p.CurrentStatus == StatusResting {
			p.Play(0, nil)
			p.CurrentStatus = StatusPlaying
		}
	}
}

// Play starts playing the current or specified song.
func (p *Player) Play(startAt int, song *Song) {

	if song == nil {
		p.CurrentSong = p.Dequeue()
		if p.CurrentSong == nil {
			slog.Info("No songs in queue")
			p.CurrentStatus = StatusResting
			return
		}
	}

	slog.Infof("Playing song: %v", p.CurrentSong.Title)
	slog.Infof("Playing song at: %v", time.Duration(startAt)*time.Second)

	config, err := config.NewConfig()
	if err != nil {
		slog.Fatalf("Error loading config: %v", err)
	}

	options := &dca.EncodeOptions{
		Volume:                  1.0,
		FrameDuration:           config.DcaFrameDuration,
		Bitrate:                 config.DcaBitrate,
		PacketLoss:              config.DcaPacketLoss,
		RawOutput:               config.DcaRawOutput,
		Application:             config.DcaApplication,
		CompressionLevel:        config.DcaCompressionLevel,
		BufferedFrames:          config.DcaBufferedFrames,
		VBR:                     config.DcaVBR,
		StartTime:               startAt,
		ReconnectAtEOF:          config.DcaReconnectAtEOF,
		ReconnectStreamed:       config.DcaReconnectStreamed,
		ReconnectOnNetworkError: config.DcaReconnectOnNetworkError,
		ReconnectOnHttpError:    config.DcaReconnectOnHttpError,
		ReconnectDelayMax:       config.DcaReconnectDelayMax,
		FfmpegBinaryPath:        config.DcaFfmpegBinaryPath,
		EncodingLineLog:         config.DcaEncodingLineLog,
		UserAgent:               config.DcaUserAgent,
	}

	p.EncodingSession, err = dca.EncodeFile(p.CurrentSong.DownloadURL, options)
	if err != nil {
		slog.Errorf("Error encoding song: %v", err)
		return
	}
	defer p.EncodingSession.Cleanup()

	for p.VoiceConnection == nil || !p.VoiceConnection.Ready {
		time.Sleep(100 * time.Millisecond)
	}

	err = p.VoiceConnection.Speaking(true)
	if err != nil {
		slog.Errorf("Error connecting to Discord voice: %v", err)
		p.VoiceConnection.Speaking(false)
		return
	}

	done := make(chan error)
	p.StreamingSession = dca.NewStream(p.EncodingSession, p.VoiceConnection, done)

	slog.Info("Stream is created, waiting for finish or error")

	p.CurrentStatus = StatusPlaying

	h := history.NewHistory()
	historySong := &history.Song{Name: p.CurrentSong.Title, UserURL: p.CurrentSong.UserURL, DownloadURL: p.CurrentSong.DownloadURL, Duration: p.CurrentSong.Duration, ID: p.CurrentSong.ID, Thumbnail: history.Thumbnail(p.CurrentSong.Thumbnail)}
	h.AddTrackToHistory(p.VoiceConnection.GuildID, historySong)

	interval := 2 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	tickerDone := make(chan bool)

	go func() {
		for {
			select {
			case <-ticker.C:
				if p.VoiceConnection != nil && p.StreamingSession != nil && p.CurrentSong != nil {
					if !p.StreamingSession.Paused() {
						err := h.AddPlaybackDurationStats(p.VoiceConnection.GuildID, p.CurrentSong.ID, float64(interval.Seconds()))
						if err != nil {
							slog.Warnf("Error adding playback duration stats to history: %v", err)
						}
					}
				}
			case <-tickerDone:
				return
			}
		}
	}()

	select {
	case <-done:
		// Auto-restarting logic in case of interruption
		// Youtube songs checked by their current vs total duration
		// Streams (radio) never stop
		if p.VoiceConnection != nil && p.StreamingSession != nil && p.CurrentSong != nil {
			if p.CurrentSong.Source != SourceStream {
				songDuration, songPosition := p.metrics(p.EncodingSession, p.StreamingSession, p.CurrentSong)
				if p.CurrentStatus == StatusPlaying {
					if p.EncodingSession.Stats().Duration.Seconds() > 0 && songPosition.Seconds() > 0 {
						if songPosition < songDuration {
							slog.Warn("Song is done but still unfinished. Restarting from interrupted position...")

							p.EncodingSession.Cleanup()
							p.VoiceConnection.Speaking(false)

							p.Play(int(songPosition.Seconds()), p.CurrentSong)

							return
						}
					}
				}
			} else {
				if p.CurrentStatus == StatusPlaying {

					slog.Warn("Song is done but its a stream so it's never finished. Restarting from interrupted position...")

					p.EncodingSession.Cleanup()
					p.VoiceConnection.Speaking(false)

					p.Play(0, p.CurrentSong)

					return

				}
			}

			err = h.AddPlaybackCountStats(p.VoiceConnection.GuildID, p.CurrentSong.ID)
			if err != nil {
				slog.Warnf("Error adding stats count stats to history: %v", err)
			}
		}

		if err != nil && err != io.EOF {
			slog.Warnf("Song is done but an unexpected error occurred: %v", err)

			time.Sleep(250 * time.Millisecond)
			if p.VoiceConnection != nil {
				p.VoiceConnection.Speaking(false)
			}
			p.CurrentStatus = StatusResting
			p.EncodingSession.Cleanup()

			return
		}

		slog.Info("Song is done")

		if len(p.GetSongQueue()) == 0 {
			slog.Info("Queue is done")

			time.Sleep(250 * time.Millisecond)
			p.Stop()

			return
		}

		time.Sleep(250 * time.Millisecond)

		slog.Info("Playing next song in queue")
		p.Play(0, nil)

	case <-p.SkipInterrupt:
		slog.Info("Song is interrupted for skip, stopping playback")

		if p.VoiceConnection != nil {
			p.VoiceConnection.Speaking(false)
		}
		p.EncodingSession.Cleanup()

		return
	}
}

// metrics calculates playback metrics for a song.
func (p *Player) metrics(encoding *dca.EncodeSession, streaming *dca.StreamingSession, song *Song) (songDuration, songPosition time.Duration) {
	encodingDuration := encoding.Stats().Duration
	encodingStartTime := time.Duration(encoding.Options().StartTime) * time.Second

	streamingPosition := streaming.PlaybackPosition()
	delay := encodingDuration - streamingPosition

	params, err := utils.ParseQueryParamsFromURL(song.DownloadURL)
	if err != nil {
		slog.Warnf("Failed to parse download URL parameters: %v", err)
	}

	// Convert duration string to time.Duration
	duration, err := time.ParseDuration(params["duration"])
	if err != nil {
		slog.Errorf("Error parsing duration:", err)
	}

	songDuration = time.Duration(duration) * time.Second
	songPosition = encodingStartTime + streamingPosition + delay

	slog.Infof("Total duration: %s, Stopped at: %s", songDuration, songPosition)
	slog.Infof("Encoding ahead of streaming: %s, Encoding started time: %s", delay, encodingStartTime)

	return songDuration, songPosition
}

// GetStatus returns the current playback status.
func (p *Player) GetCurrentStatus() PlaybackStatus {
	return p.CurrentStatus
}

// SetStatus sets the playback status.
func (p *Player) SetCurrentStatus(status PlaybackStatus) {
	p.Lock()
	defer p.Unlock()
	p.CurrentStatus = status
}

// GetSongQueue returns the song queue.
func (p *Player) GetSongQueue() []*Song {
	return p.SongQueue
}

// GetVoiceConnection returns the voice connection.
func (p *Player) GetVoiceConnection() *discordgo.VoiceConnection {
	return p.VoiceConnection
}

// SetVoiceConnection sets the voice connection.
func (p *Player) SetVoiceConnection(voiceConnection *discordgo.VoiceConnection) {
	p.Lock()
	defer p.Unlock()
	p.VoiceConnection = voiceConnection
}

// GetCurrentSong returns the current song being played.
func (p *Player) GetCurrentSong() *Song {
	return p.CurrentSong
}

// GetStreamingSession returns the current streaming session.
func (p *Player) GetStreamingSession() *dca.StreamingSession {
	return p.StreamingSession
}
