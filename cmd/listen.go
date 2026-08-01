package cmd

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/celados/vox/internal/audio"
	"github.com/celados/vox/internal/config"
	"github.com/celados/vox/internal/dashscope"
	"github.com/celados/vox/internal/ui"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type ListenCmd struct {
	Channel []string `short:"c" help:"Channel names or IDs to listen to (repeatable, default: all)"`
	Voice   string   `short:"v" help:"Default voice for TTS"`
	Speed   float64  `short:"s" default:"1.2" help:"Speech rate (0.5-2.0)"`
	NoChime bool     `help:"Disable notification chime sound"`
	Ignore  []string `help:"User IDs or display names to ignore (repeatable)"`
}

func (c *ListenCmd) Run(cfg *config.AppConfig) error {
	apiKey, err := cfg.RequireAPIKey()
	if err != nil {
		return err
	}
	botToken, appToken, err := cfg.RequireSlack()
	if err != nil {
		return err
	}

	// Default voice
	defaultVoice := c.Voice
	if defaultVoice == "" {
		defaultVoice = cfg.State.LastVoice
	}
	if defaultVoice == "" {
		defaultVoice = "Cherry"
	}

	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	client := socketmode.New(api, socketmode.OptionLog(log.New(os.Stderr, "", 0)))

	// Get bot's own user ID to skip self messages
	authResp, err := api.AuthTest()
	if err != nil {
		return fmt.Errorf("slack auth test: %w", err)
	}
	botUserID := authResp.UserID

	// Resolve channel filters: CLI flag > config > all
	channels := c.Channel
	if len(channels) == 0 {
		channels = cfg.Config.Listen.Channels
	}
	filterChannels := map[string]bool{}
	for _, ch := range channels {
		id := resolveChannel(api, ch)
		filterChannels[id] = true
	}

	// Build caches
	userNames := map[string]string{}
	channelNames := map[string]string{}

	getName := func(userID string) string {
		if name, ok := userNames[userID]; ok {
			return name
		}
		if info, err := api.GetUserInfo(userID); err == nil {
			name := info.Profile.DisplayName
			if name == "" {
				name = info.RealName
			}
			if name == "" {
				name = info.Name
			}
			userNames[userID] = name
			return name
		}
		return "someone"
	}

	getChannelName := func(channelID string) string {
		if name, ok := channelNames[channelID]; ok {
			return name
		}
		if info, err := api.GetConversationInfo(&slack.GetConversationInfoInput{ChannelID: channelID}); err == nil {
			channelNames[channelID] = info.Name
			return info.Name
		}
		return channelID
	}

	// Build ignore set: CLI flag > config
	ignoreList := c.Ignore
	if len(ignoreList) == 0 {
		ignoreList = cfg.Config.Listen.Ignore
	}
	ignoreUsers := map[string]bool{}
	for _, u := range ignoreList {
		ignoreUsers[strings.ToLower(u)] = true
	}

	// Voice mapping from config: user display name or ID → voice
	voiceMap := cfg.Config.Listen.VoiceMap

	// Returns (voice, mapped). mapped=true means the user has a dedicated voice.
	resolveVoice := func(userID, displayName string) (string, bool) {
		if v, ok := voiceMap[userID]; ok {
			return v, true
		}
		if v, ok := voiceMap[displayName]; ok {
			return v, true
		}
		lower := strings.ToLower(displayName)
		for k, v := range voiceMap {
			if strings.ToLower(k) == lower {
				return v, true
			}
		}
		return defaultVoice, false
	}

	ttsClient := dashscope.NewRealtimeClient(apiKey)

	ui.Success("Listening on Slack")
	ui.KV("Voice", defaultVoice)
	if len(filterChannels) > 0 {
		ui.KV("Channels", strings.Join(channels, ", "))
	} else {
		ui.KV("Channels", "all")
	}
	if len(ignoreUsers) > 0 {
		ui.KV("Ignore", strings.Join(ignoreList, ", "))
	}
	if len(voiceMap) > 0 {
		ui.KV("Voice map", fmt.Sprintf("%d users", len(voiceMap)))
	}
	ui.Info("%s", ui.Dim("Ctrl+C to stop"))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() {
		for evt := range client.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				client.Ack(*evt.Request)

				switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					// Skip bot's own messages, bot messages, and edits
					if ev.User == botUserID || ev.BotID != "" || ev.SubType != "" {
						continue
					}

					// Channel filter
					if len(filterChannels) > 0 && !filterChannels[ev.Channel] {
						continue
					}

					// Ignore list: match by user ID or display name (case-insensitive)
					if ignoreUsers[strings.ToLower(ev.User)] || ignoreUsers[strings.ToLower(getName(ev.User))] {
						continue
					}

					text := ev.Text
					if text == "" {
						continue
					}

					text = cleanSlackText(text)
					sender := getName(ev.User)
					chName := getChannelName(ev.Channel)
					voice, mapped := resolveVoice(ev.User, sender)
					model := dashscope.ModelForVoice(voice)

					// If voice is mapped to this user, skip the "from X in Y" fence —
					// the voice itself identifies who's speaking.
					var spoken string
					if mapped {
						spoken = text
					} else {
						spoken = fmt.Sprintf("%s. From %s, in %s.", text, sender, chName)
					}

					ui.Info("%s %s [%s] %s: %s",
						ui.Dim(time.Now().Format("15:04")),
						ui.Dim(chName),
						ui.Key(voice),
						ui.Key(sender),
						text,
					)

					// Chime before message (skip if voice is mapped)
					if !c.NoChime && !mapped {
						playChime()
					}

					// Speak it
					player := audio.NewStreamPlayer()
					opts := dashscope.TTSOptions{
						Model:      model,
						Voice:      voice,
						Text:       spoken,
						Lang:       "auto",
						SpeechRate: c.Speed,
					}
					ttsCtx, ttsCancel := context.WithTimeout(context.Background(), 30*time.Second)
					ttsClient.StreamTTS(ttsCtx, opts, func(pcm []byte) {
						player.Write(pcm)
					})
					player.Close()
					ttsCancel()
				}

			case socketmode.EventTypeConnectionError:
				ui.Warn("Connection error, reconnecting...")

			case socketmode.EventTypeConnecting:
				ui.Info("%s", ui.Dim("connecting..."))

			case socketmode.EventTypeConnected:
				ui.Info("%s", ui.Dim("connected"))
			}
		}
	}()

	go client.RunContext(ctx)

	<-ctx.Done()
	ui.Info("\n%s", ui.Dim("stopped"))
	return nil
}

func resolveChannel(api *slack.Client, channel string) string {
	channel = strings.TrimPrefix(channel, "#")
	// If it looks like an ID already
	if strings.HasPrefix(channel, "C") || strings.HasPrefix(channel, "G") || strings.HasPrefix(channel, "D") {
		return channel
	}
	params := &slack.GetConversationsParameters{Limit: 200}
	channels, _, err := api.GetConversations(params)
	if err != nil {
		return channel
	}
	for _, ch := range channels {
		if ch.Name == channel {
			return ch.ID
		}
	}
	return channel
}

// playChime generates a short notification tone (two-tone chime)
func playChime() {
	const (
		sampleRate = audio.SampleRate // 24000
		duration   = 120             // ms total
		freq1      = 880             // A5
		freq2      = 1320            // E6
	)

	samples := sampleRate * duration / 1000
	pcm := make([]byte, samples*2) // 16-bit

	half := samples / 2
	for i := range samples {
		freq := float64(freq1)
		if i >= half {
			freq = float64(freq2)
		}

		// Fade envelope
		var env float64
		pos := i % half
		total := half
		if pos < total/4 {
			env = float64(pos) / float64(total/4) // attack
		} else {
			env = 1.0 - float64(pos-total/4)/float64(total*3/4) // decay
		}

		t := float64(i) / float64(sampleRate)
		sample := int16(env * 3000 * math.Sin(2*math.Pi*freq*t))
		pcm[i*2] = byte(sample)
		pcm[i*2+1] = byte(sample >> 8)
	}

	player := audio.NewStreamPlayer()
	player.Write(pcm)
	player.Close()
}

// cleanSlackText removes slack markup like <@U123> mentions, <url|label> links, etc.
func cleanSlackText(text string) string {
	result := text
	// Replace user mentions <@U123>
	for {
		start := strings.Index(result, "<@")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], ">")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}

	// Replace <url|label> with label, or <url> with url
	for {
		start := strings.Index(result, "<")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], ">")
		if end == -1 {
			break
		}
		inner := result[start+1 : start+end]
		if pipe := strings.Index(inner, "|"); pipe != -1 {
			inner = inner[pipe+1:]
		}
		result = result[:start] + inner + result[start+end+1:]
	}

	return strings.TrimSpace(result)
}
