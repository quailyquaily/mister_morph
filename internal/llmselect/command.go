package llmselect

import (
	"errors"
	"regexp"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/llmutil"
)

type CommandAction string

const (
	CommandCurrent CommandAction = "current"
	CommandList    CommandAction = "list"
	CommandSet     CommandAction = "set"
	CommandReset   CommandAction = "reset"
)

type Command struct {
	Action      CommandAction
	ProfileName string
}

var slackMentionPrefixPattern = regexp.MustCompile(`^<@[^>]+>$`)

func ParseCommand(text string) (Command, bool, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return Command{}, false, nil
	}
	if slackMentionPrefixPattern.MatchString(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return Command{}, false, nil
	}
	cmdWord := normalizeCommandWord(fields[0])
	if cmdWord != "/models" {
		return Command{}, false, nil
	}
	switch len(fields) {
	case 1:
		return Command{Action: CommandCurrent}, true, nil
	case 2:
		switch strings.ToLower(fields[1]) {
		case "list":
			return Command{Action: CommandList}, true, nil
		case "reset":
			return Command{Action: CommandReset}, true, nil
		default:
			return Command{}, true, errors.New(UsageText())
		}
	case 3:
		if strings.ToLower(fields[1]) != "set" {
			return Command{}, true, errors.New(UsageText())
		}
		return Command{
			Action:      CommandSet,
			ProfileName: fields[2],
		}, true, nil
	default:
		return Command{}, true, errors.New(UsageText())
	}
}

func normalizeCommandWord(word string) string {
	if !strings.HasPrefix(word, "/") {
		return word
	}
	if at := strings.IndexByte(word, '@'); at >= 0 {
		word = word[:at]
	}
	return strings.ToLower(word)
}

func ExecuteCommandText(values llmutil.RuntimeValues, store *Store, text string) (string, bool, error) {
	cmd, handled, err := ParseCommand(text)
	if !handled || err != nil {
		return "", handled, err
	}
	output, err := ExecuteCommand(values, store, cmd)
	if err != nil {
		return "", true, err
	}
	return output, true, nil
}

func ExecuteCommand(values llmutil.RuntimeValues, store *Store, cmd Command) (string, error) {
	if store == nil {
		store = NewStore()
	}
	switch cmd.Action {
	case CommandCurrent:
		view, err := GetSelection(values, store.Get())
		if err != nil {
			return "", err
		}
		return RenderSelectionText(view), nil
	case CommandList:
		profiles, err := ListProfiles(values)
		if err != nil {
			return "", err
		}
		return RenderProfilesText(profiles), nil
	case CommandSet:
		profile, err := ValidateProfile(values, cmd.ProfileName)
		if err != nil {
			return "", err
		}
		store.SetProfile(profile.Name)
		return RenderSetText(profile), nil
	case CommandReset:
		store.Reset()
		view, err := GetSelection(values, store.Get())
		if err != nil {
			return "", err
		}
		return RenderResetText(view), nil
	default:
		return "", errors.New(UsageText())
	}
}
