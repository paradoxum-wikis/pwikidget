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
	appID   string
	token   string
	store   *Store
	wiki    *WikiClient
	aphonos *AphonosClient
}

var widgetCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "widget",
		Description: "ALTER EGO & TDS wiki profile widget",
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
		data := i.MessageComponentData()
		switch {
		case strings.HasPrefix(data.CustomID, "setup:continue"):
			b.handleSetupContinue(s, i)
		case strings.HasPrefix(data.CustomID, "verify:"):
			b.handleVerify(s, i)
		}
	}
}

func (b *Bot) handleSetup(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	username := ""
	for _, o := range opts {
		if o.Name == "username" {
			username = o.StringValue()
		}
	}
	b.showSetupAck(s, i, username)
}

const setupAckMessage = "As of **June 4th**, Discord restricted profile widgets, so you must be **invited** to use this one.\n\n" +
	"Contact <@380694434980954114> for an invite, and enable **2FA** on your Discord account before continuing."

func (b *Bot) showSetupAck(s *discordgo.Session, i *discordgo.InteractionCreate, username string) {
	customID := "setup:continue"
	if username != "" {
		customID = "setup:continue:" + username
	}
	respondComponents(s, i, setupAckMessage, []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Continue",
				Style:    discordgo.PrimaryButton,
				CustomID: customID,
			},
		}},
	}, true)
}

func (b *Bot) handleSetupContinue(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := i.MessageComponentData().CustomID
	username := ""
	if parts := strings.SplitN(customID, ":", 3); len(parts) == 3 {
		username = parts[2]
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		return
	}

	uid := discordUserID(i)

	link, err := b.aphonos.Lookup(uid)
	if err != nil {
		followup(s, i, "Failed to check Aphonos links: "+err.Error())
		return
	}
	if link != nil {
		if err := b.wiki.ValidateUser(link.FandomUsername, link.FandomUserID); err != nil {
			followup(s, i, err.Error())
			return
		}
		b.linkAndSync(s, i, uid, link.FandomUsername, link.FandomUserID)
		return
	}

	if username == "" {
		followup(s, i, "Not linked via Aphonos. Run `/widget setup username:YourName`.")
		return
	}

	token := verificationToken(b.token, uid)
	profileURL := fmt.Sprintf("https://%s/wiki/User:%s", wikiAE.Domain, urlPathEscape(username))

	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf(
			"Add this to your **ALTERPEDIA** profile bio, then click **Verify**:\n`%s`",
			token,
		),
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label: "ALTERPEDIA Profile",
					Style: discordgo.LinkButton,
					URL:   profileURL,
				},
				discordgo.Button{
					Label:    "Verify",
					Style:    discordgo.PrimaryButton,
					CustomID: fmt.Sprintf("verify:%s", username),
				},
			}},
		},
		Flags: discordgo.MessageFlagsEphemeral,
	})
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
	aeID, err := b.wiki.LookupUserID(wikiAE, username)
	if err != nil {
		followup(s, i, err.Error())
		return
	}

	profile, err := b.wiki.GetFandomProfile(wikiAE, aeID)
	if err != nil {
		followup(s, i, err.Error())
		return
	}

	expected := verificationToken(b.token, uid)
	if !strings.Contains(profile.Bio, expected) {
		followup(s, i, "Verification failed. Put the exact token in your ALTERPEDIA profile bio and try again.")
		return
	}

	if _, err := b.wiki.GetIntegratedProfile(wikiTDS, aeID, username); err != nil {
		followup(s, i, fmt.Errorf("tds: %w", err).Error())
		return
	}

	b.linkAndSync(s, i, uid, username, aeID)
}

func (b *Bot) linkAndSync(s *discordgo.Session, i *discordgo.InteractionCreate, uid, username string, userID int64) {
	link := UserLink{
		Username: username,
		UserID:   userID,
	}
	if err := b.store.Save(uid, link); err != nil {
		followup(s, i, "Failed to save link.")
		return
	}

	if err := b.syncUser(uid, link); err != nil {
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

	if err := b.syncUser(uid, link); err != nil {
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
