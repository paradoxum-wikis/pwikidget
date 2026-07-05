package internal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	appID  string
	token  string
	store  *Store
	fandom *FandomClient
	aphonos *AphonosClient
}

var widgetCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "widget",
		Description: "Alter-Ego & TDS wiki profile widget",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "setup",
				Description: "Link your Fandom account",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "username",
						Description: "Your Fandom username (not needed if linked via Aphonos)",
						Required:    false,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "refresh",
				Description: "Refresh your widget data",
			},
		},
		IntegrationTypes: new([]discordgo.ApplicationIntegrationType{discordgo.ApplicationIntegrationUserInstall}),
		Contexts: new([]discordgo.InteractionContextType{
			discordgo.InteractionContextGuild,
			discordgo.InteractionContextBotDM,
			discordgo.InteractionContextPrivateChannel,
		}),
	},
}

func (b *Bot) onReady(s *discordgo.Session, _ *discordgo.Ready) {
	if _, err := s.ApplicationCommandBulkOverwrite(b.appID, "", widgetCommands); err != nil {
		log.Println("register commands:", err)
		return
	}
	log.Println("registered commands")
}

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		data := i.ApplicationCommandData()
		if data.Name != "widget" || len(data.Options) == 0 {
			return
		}
		switch data.Options[0].Name {
		case "setup":
			b.handleSetup(s, i, data.Options[0].Options)
		case "refresh":
			b.handleRefresh(s, i)
		}
	case discordgo.InteractionMessageComponent:
		if strings.HasPrefix(i.MessageComponentData().CustomID, "verify:") {
			b.handleVerify(s, i)
		}
	}
}

func (b *Bot) handleSetup(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	uid := discordUserID(i)

	link, err := b.aphonos.Lookup(uid)
	if err != nil {
		respond(s, i, "Failed to check Aphonos links: "+err.Error(), true)
		return
	}
	if link != nil {
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
		}); err != nil {
			return
		}
		b.finishSetup(s, i, uid, link.FandomUsername, link.FandomUserID)
		return
	}

	username := ""
	for _, o := range opts {
		if o.Name == "username" {
			username = o.StringValue()
		}
	}
	if username == "" {
		respond(s, i, "Not linked via Aphonos. Run `/widget setup username:YourName`.", true)
		return
	}

	if _, err := b.fandom.LookupUserID(wikiAE, username); err != nil {
		respond(s, i, err.Error(), true)
		return
	}

	token := verificationToken(b.token, uid)
	profileURL := fmt.Sprintf("https://%s.fandom.com/wiki/User:%s", wikiAE, urlPathEscape(username))

	respondComponents(s, i, fmt.Sprintf(
		"Add this to your **Alter-Ego** profile bio, then click **Verify**:\n`%s`",
		token,
	), []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label: "Alter-Ego Profile",
				Style: discordgo.LinkButton,
				URL:   profileURL,
			},
			discordgo.Button{
				Label:    "Verify",
				Style:    discordgo.PrimaryButton,
				CustomID: fmt.Sprintf("verify:%s", username),
			},
		}},
	}, true)
}

func (b *Bot) handleVerify(s *discordgo.Session, i *discordgo.InteractionCreate) {
	parts := strings.SplitN(i.MessageComponentData().CustomID, ":", 2)
	if len(parts) != 2 {
		respond(s, i, "Invalid verify button.", true)
		return
	}
	username := parts[1]

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		return
	}

	uid := discordUserID(i)
	aeID, err := b.fandom.LookupUserID(wikiAE, username)
	if err != nil {
		followup(s, i, err.Error())
		return
	}

	profile, err := b.fandom.GetProfile(wikiAE, aeID)
	if err != nil {
		followup(s, i, err.Error())
		return
	}

	expected := verificationToken(b.token, uid)
	if !strings.Contains(profile.Bio, expected) {
		followup(s, i, "Verification failed. Put the exact token in your Alter-Ego profile bio and try again.")
		return
	}

	b.finishSetup(s, i, uid, username, aeID)
}

func (b *Bot) finishSetup(s *discordgo.Session, i *discordgo.InteractionCreate, uid, username string, userID int64) {
	aeID, tdsID, err := b.fandom.ResolveOnWikis(username, userID)
	if err != nil {
		followup(s, i, err.Error())
		return
	}

	link := UserLink{
		DiscordID: uid,
		Username:  username,
		AEUserID:  aeID,
		TDSUserID: tdsID,
	}
	if err := b.store.Save(link); err != nil {
		followup(s, i, "Failed to save link.")
		return
	}

	if err := b.syncUser(link); err != nil {
		b.followupSyncError(s, i, "Linked, but widget sync failed: ", err)
		return
	}
	followup(s, i, "Linked and synced! Widget auto-refreshes every 30 minutes.")
}

func (b *Bot) handleRefresh(s *discordgo.Session, i *discordgo.InteractionCreate) {
	uid := discordUserID(i)
	link, err := b.store.Get(uid)
	if err != nil {
		respond(s, i, "Run `/widget setup` first.", true)
		return
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		return
	}

	if err := b.syncUser(link); err != nil {
		b.followupSyncError(s, i, "Sync failed: ", err)
		return
	}
	followup(s, i, "Widget refreshed!")
}

func discordUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	return i.User.ID
}

func verificationToken(secret, discordID string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(discordID))
	return "pwikidget-" + hex.EncodeToString(m.Sum(nil))
}

func respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content, Flags: flags},
	})
}

func respondComponents(s *discordgo.Session, i *discordgo.InteractionCreate, content string, components []discordgo.MessageComponent, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: components,
			Flags:      flags,
		},
	})
}

func followup(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: content,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

func oauthAuthorizeComponents(appID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label: "Authorize",
				Style: discordgo.LinkButton,
				URL:   widgetAuthURL(appID),
			},
		}},
	}
}

func (b *Bot) followupSyncError(s *discordgo.Session, i *discordgo.InteractionCreate, prefix string, err error) {
	log.Println("sync:", err)
	if errors.Is(err, ErrOAuthRequired) {
		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content:    prefix + "click **Authorize**, then run `/widget refresh`.",
			Components: oauthAuthorizeComponents(b.appID),
			Flags:      discordgo.MessageFlagsEphemeral,
		})
		return
	}
	followup(s, i, prefix+err.Error())
}
