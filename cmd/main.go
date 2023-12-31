package main

import (
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	"github.com/gookit/slog"
	"github.com/gookit/slog/handler"

	"github.com/keshon/melodix-discord-player/internal/config"
	"github.com/keshon/melodix-discord-player/internal/db"
	"github.com/keshon/melodix-discord-player/internal/manager"
	"github.com/keshon/melodix-discord-player/internal/rest"
	"github.com/keshon/melodix-discord-player/internal/version"
	"github.com/keshon/melodix-discord-player/music/discord"
)

var botInstances map[string]*discord.BotInstance

func main() {
	slog.Configure(func(logger *slog.SugaredLogger) {
		f := logger.Formatter.(*slog.TextFormatter)
		f.EnableColor = true
		f.SetTemplate("[{{datetime}}] [{{level}}] [{{caller}}]\t{{message}} {{data}} {{extra}}\n")
		f.ColorTheme = slog.ColorTheme
	})

	h1 := handler.MustFileHandler("./logs/all-levels.log", handler.WithLogLevels(slog.AllLevels))
	slog.PushHandler(h1)

	// logger := slog.Std()

	config, err := config.NewConfig()
	if err != nil {
		slog.Fatalf("Error loading config: %v", err)
		os.Exit(0)
	}

	slog.Info("Config loaded:\n" + config.String())

	if _, err := db.InitDB("./melodix.db"); err != nil {
		slog.Fatalf("Error initializing the database: %v", err)
		os.Exit(0)
	}

	dg, err := discordgo.New("Bot " + config.DiscordBotToken)
	if err != nil {
		slog.Fatalf("Error creating Discord session: %v", err)
		os.Exit(0)
	}

	botInstances = make(map[string]*discord.BotInstance)

	guildManager := manager.NewGuildManager(dg, botInstances)
	guildManager.Start()

	guildIDs, err := getGuildsOrSetDefault()
	if err != nil {
		slog.Fatalf("Error retrieving or creating guilds: %v", err)
		os.Exit(0)
	}

	for _, guildID := range guildIDs {
		startBotInstances(dg, guildID)
	}

	if err := dg.Open(); err != nil {
		slog.Fatalf("Error opening Discord session: %v", err)
		os.Exit(0)
	}
	defer dg.Close()

	if config.RestEnabled {
		startRestServer(config.RestGinRelease, config.RestHostname)
	}

	slog.Infof("%v is now running. Press Ctrl+C to exit", version.AppName)

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
}

func getGuildsOrSetDefault() ([]string, error) {
	guildIDs, err := db.GetAllGuildIDs()
	if err != nil {
		return nil, err
	}

	if len(guildIDs) == 0 {
		guild := db.Guild{ID: "897053062030585916", Name: "default"} // TODO: default guild id is so-so
		if err := db.CreateGuild(guild); err != nil {
			return nil, err
		}
		guildIDs = append(guildIDs, guild.ID)
	}

	return guildIDs, nil
}

func startBotInstances(session *discordgo.Session, guildID string) {
	botInstances[guildID] = &discord.BotInstance{
		Melodix: discord.NewDiscord(session, guildID),
	}
	botInstances[guildID].Melodix.Start(guildID)
}

func startRestServer(isReleaseMode bool, hostname string) {
	if isReleaseMode {
		gin.SetMode("release")
	}

	router := gin.Default()

	restAPI := rest.NewRest(botInstances)
	restAPI.Start(router)

	go func() {
		// parse hostname var - if it has port - use it or fallback to 8080
		host, port, err := net.SplitHostPort(hostname)
		if err != nil {
			// If there's an error, assume the entire input is the host (without port)
			host = hostname
			port = "8080"
		}

		// If hostname is empty, set it to the default port (8080)
		if host == "" {
			host = "localhost"
		}

		slog.Infof("REST API server started on %s:%s\n", host, port)
		if err := router.Run(net.JoinHostPort(host, port)); err != nil {
			slog.Fatalf("Error starting REST API server: %v", err)
		}
	}()
}
