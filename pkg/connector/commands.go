package connector

import (
	"fmt"
	"regexp"
	"strings"

	"maunium.net/go/mautrix/bridgev2/commands"
)

var cmdEmoticons = &commands.FullHandler{
	Func:    fnEmoticons,
	Name:    "emoticons",
	Aliases: []string{"emoticon", "이모티콘"},
	Help: commands.HelpMeta{
		Section:     commands.HelpSectionMisc,
		Description: "Show your KakaoTalk emoticon room, or add a pack to it (forward an emoticon from that room to send it)",
		Args:        "[add <pack number or emoticon file name> | sync | sheet]",
	},
	RequiresLogin: true,
}

// packArg accepts a pack number ("4446367") or an emoticon file name
// ("4446367.emot_005.webp").
var packArg = regexp.MustCompile(`^(\d{3,12})(?:\.emot_\d{3}\.(?:webp|png|gif))?$`)

func fnEmoticons(ce *commands.Event) {
	login := ce.User.GetDefaultLogin()
	if login == nil {
		ce.Reply("You're not logged in to KakaoTalk.")
		return
	}
	kc, ok := login.Client.(*KakaoClient)
	if !ok || kc.Main.emoticons == nil {
		ce.Reply("Emoticons aren't available on this bridge.")
		return
	}
	loginID := string(kc.UserLogin.ID)
	switch {
	case len(ce.Args) == 0:
		roomID, err := kc.Main.emoticons.shelfRoom(ce.Ctx, loginID)
		if err != nil {
			ce.Reply("Failed to look up the emoticon room: %v", err)
			return
		}
		packs, err := kc.Main.emoticons.shelfPacks(ce.Ctx, loginID)
		if err != nil {
			ce.Reply("Failed to list emoticon packs: %v", err)
			return
		}
		if roomID == "" || len(packs) == 0 {
			ce.Reply("Your emoticon room is empty. Send an emoticon from KakaoTalk (its pack is added automatically), " +
				"or use `$cmdprefix emoticons add <pack number>`.")
			return
		}
		lines := make([]string, 0, len(packs))
		for _, p := range packs {
			lines = append(lines, fmt.Sprintf("* %s (%d)", p.Pack, p.Count))
		}
		ce.Reply("Your emoticon room is [KakaoTalk 이모티콘](%s). Long-press an emoticon there and forward it to a KakaoTalk chat.\n\nPacks:\n%s",
			roomID.URI(kc.Main.Bridge.Matrix.ServerName()).MatrixToURL(), strings.Join(lines, "\n"))
	case len(ce.Args) == 2 && ce.Args[0] == "add":
		m := packArg.FindStringSubmatch(ce.Args[1])
		if m == nil {
			ce.Reply("Give a pack number, like `$cmdprefix emoticons add 4446367`, or an emoticon file name like `4446367.emot_005.webp`.")
			return
		}
		ce.Reply("Adding pack %s…", m[1])
		n, err := kc.addPackToShelf(ce.Ctx, m[1])
		if err != nil {
			ce.Reply("Failed to add pack %s: %v", m[1], err)
			return
		}
		ce.Reply("Pack %s is in your emoticon room (%d emoticons).", m[1], n)
	case len(ce.Args) == 1 && ce.Args[0] == "sync":
		kc.syncShelf(ce.Ctx)
		ce.Reply("Adding any packs you've used that aren't in the emoticon room yet.")
	case len(ce.Args) == 1 && ce.Args[0] == "sheet":
		if err := kc.Main.emoticons.clearSheets(ce.Ctx, loginID); err != nil {
			ce.Reply("Failed to reset the custom emoji sheets: %v", err)
			return
		}
		kc.shelfLock.Lock()
		err := kc.postMissingSheets(ce.Ctx)
		kc.shelfLock.Unlock()
		if err != nil {
			ce.Reply("Failed to post the custom emoji sheets: %v", err)
			return
		}
		ce.Reply("Posted the custom emoji sheets in your emoticon room.")
	default:
		ce.Reply("Usage: `$cmdprefix emoticons [add <pack number> | sync | sheet]`")
	}
}
