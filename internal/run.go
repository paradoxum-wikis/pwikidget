package internal

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

func Run(configPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	appID, err := resolveAppID(cfg.DiscordToken)
	if err != nil {
		return err
	}

	store, err := openStore("links.json")
	if err != nil {
		return err
	}

	fandom := newFandomClient("pwikidget/1.0 (+https://github.com/fandomwidget/pwikidget)")

	s, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return err
	}

	bot := &Bot{
		appID:   appID,
		token:   cfg.DiscordToken,
		store:   store,
		fandom:  fandom,
		aphonos: newAphonosClient(),
	}

	s.AddHandler(bot.onReady)
	s.AddHandler(bot.onInteraction)

	if err := s.Open(); err != nil {
		return err
	}
	defer s.Close()

	bot.startAutoSync()

	fmt.Println("Widget auth URL:")
	fmt.Println(widgetAuthURL(appID))
	fmt.Println("pwikidget is running")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc
	return nil
}
