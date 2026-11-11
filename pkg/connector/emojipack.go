package connector

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Beeper has no sticker picker for bridges, but its emoji picker has a "Custom"
// tab (the user's m.image_pack account data). A bridge can't write that account
// data on Beeper, so for each pack the shelf room gets a "sheet": one message
// listing the pack, with the bot's reaction of each emoticon as a custom emoji
// (key = mxc URI, com.beeper.reaction.shortcode = name). Right-clicking such a
// reaction in Beeper Desktop offers "Add :name: to Custom Emoji" (inline custom
// emoji in bridged rooms don't get that menu). The custom emoji keep the shelf's
// mxc URIs, so a message made of one of them is sent as the real emoticon by
// HandleMatrixMessage.

// emojiShortcode is the custom emoji name for an emoticon path, e.g.
// "4446367.emot_005.webp" -> "kakao4446367_05".
func emojiShortcode(path string) string {
	m := emoticonPathParts.FindStringSubmatch(path)
	if m == nil {
		return ""
	}
	n := strings.TrimLeft(m[2], "0")
	if len(n) < 2 {
		n = strings.Repeat("0", 2-len(n)) + n
	}
	return "kakao" + m[1] + "_" + n
}

var emoticonPathParts = regexp.MustCompile(`^(\d+)\.emot_(\d{3})\.(?:webp|png|gif)$`)

const sheetIntro = "Custom emoji: in Beeper Desktop, right-click each reaction under this message and choose “Add to Custom Emoji”. " +
	"Then pick it from the Custom tab of the emoji button in any KakaoTalk chat; on its own or with text, it goes out as the real emoticon."

// emojiSheet is the message content listing a pack's emoticons as custom emoji.
func emojiSheet(pack string, imgs []packImage) *event.MessageEventContent {
	var body, fmtBody strings.Builder
	body.WriteString(sheetIntro + "\n\n")
	fmtBody.WriteString("<p>" + html.EscapeString(sheetIntro) + "</p><p>")
	for _, img := range imgs {
		code := emojiShortcode(img.Path)
		if code == "" {
			continue
		}
		name := html.EscapeString(":" + code + ":")
		body.WriteString(":" + code + ": ")
		fmt.Fprintf(&fmtBody, `<img data-mx-emoticon src="%s" alt="%s" title="%s" height="64" /> `,
			html.EscapeString(string(img.MXC)), name, name)
	}
	fmtBody.WriteString("</p>")
	return &event.MessageEventContent{
		MsgType:       event.MsgNotice,
		Body:          strings.TrimSpace(body.String()),
		Format:        event.FormatHTML,
		FormattedBody: fmtBody.String(),
	}
}

// postMissingSheets posts the sheet of every shelf pack that has none yet. The
// caller holds shelfLock.
func (kc *KakaoClient) postMissingSheets(ctx context.Context) error {
	if !kc.Main.Config.EmoticonPicker || kc.Main.emoticons == nil {
		return nil
	}
	loginID := string(kc.UserLogin.ID)
	roomID, err := kc.Main.emoticons.shelfRoom(ctx, loginID)
	if err != nil || roomID == "" {
		return err
	}
	packs, err := kc.Main.emoticons.shelfPacks(ctx, loginID)
	if err != nil {
		return err
	}
	for _, p := range packs {
		if posted, err := kc.Main.emoticons.sheetPosted(ctx, loginID, p.Pack); err != nil || posted {
			if err != nil {
				return err
			}
			continue
		}
		imgs, err := kc.Main.emoticons.packImages(ctx, p.Pack)
		if err != nil {
			return err
		} else if len(imgs) == 0 {
			continue
		}
		bot := kc.Main.Bridge.Bot
		resp, err := bot.SendMessage(ctx, roomID, event.EventMessage, &event.Content{Parsed: emojiSheet(p.Pack, imgs)}, nil)
		if err != nil {
			return fmt.Errorf("failed to post the custom emoji sheet of %s: %w", p.Pack, err)
		}
		for _, img := range imgs {
			code := emojiShortcode(img.Path)
			if code == "" {
				continue
			}
			_, err = bot.SendMessage(ctx, roomID, event.EventReaction, &event.Content{
				Parsed: &event.ReactionEventContent{RelatesTo: event.RelatesTo{
					Type: event.RelAnnotation, EventID: resp.EventID, Key: string(img.MXC),
				}},
				Raw: map[string]any{"com.beeper.reaction.shortcode": ":" + code + ":"},
			}, nil)
			if err != nil {
				return fmt.Errorf("failed to react with %s: %w", code, err)
			}
		}
		if err = kc.Main.emoticons.setSheetPosted(ctx, loginID, p.Pack); err != nil {
			return err
		}
		zerolog.Ctx(ctx).Info().Str("pack", p.Pack).Int("emoticons", len(imgs)).Msg("Posted the custom emoji sheet")
	}
	return nil
}

var (
	htmlImgTag  = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	htmlSrcAttr = regexp.MustCompile(`(?i)\bsrc\s*=\s*"([^"]*)"`)
	htmlBreak   = regexp.MustCompile(`(?i)<br\s*/?>|</p>\s*<p[^>]*>`)
	htmlTag     = regexp.MustCompile(`<[^>]+>`)
)

// singleCustomEmoji finds a message holding exactly one custom emoji (an
// <img data-mx-emoticon>) and returns its mxc URI and the rest of the text.
func singleCustomEmoji(content *event.MessageEventContent) (id.ContentURIString, string, bool) {
	if content.Format != event.FormatHTML || content.FormattedBody == "" {
		return "", "", false
	}
	imgs := htmlImgTag.FindAllStringIndex(content.FormattedBody, -1)
	if len(imgs) != 1 {
		return "", "", false
	}
	tag := content.FormattedBody[imgs[0][0]:imgs[0][1]]
	if !strings.Contains(strings.ToLower(tag), "data-mx-emoticon") {
		return "", "", false
	}
	m := htmlSrcAttr.FindStringSubmatch(tag)
	if m == nil || !strings.HasPrefix(m[1], "mxc://") {
		return "", "", false
	}
	rest := content.FormattedBody[:imgs[0][0]] + content.FormattedBody[imgs[0][1]:]
	rest = htmlBreak.ReplaceAllString(rest, "\n")
	rest = html.UnescapeString(htmlTag.ReplaceAllString(rest, ""))
	return id.ContentURIString(html.UnescapeString(m[1])), strings.TrimSpace(rest), true
}
