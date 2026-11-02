package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
	"github.com/spf13/cobra"
)

type editDocument struct {
	Project string   `json:"project"`
	Start   string   `json:"start"`
	Stop    string   `json:"stop,omitempty"`
	Tags    []string `json:"tags"`
	Message string   `json:"message,omitempty"`
}

func (a *app) edit() *cobra.Command {
	var confirmProject, confirmTag bool
	var message string
	cmd := &cobra.Command{Use: "edit [ID]", Short: "Edit a frame.", Args: cobra.MaximumNArgs(1), FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true}, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := a.openData()
		if err != nil {
			return err
		}
		ref := ""
		if len(args) > 0 {
			ref = args[0]
		}
		editingCurrent := ref == "" && s.State.Running()
		var frame store.Frame
		if editingCurrent {
			frame = store.Frame{Start: s.State.Start, Project: s.State.Project, Tags: s.State.Tags, Message: s.Messages.Timers[watson.PrimaryTimerID]}
		} else {
			if ref == "" {
				ref = "-1"
			}
			frame, _, err = s.Lookup(ref)
			if err != nil {
				if len(s.Frames) == 0 && len(args) == 0 {
					return errors.New("No frames recorded yet. It's time to create your first one!")
				}
				return err
			}
		}
		if cmd.Flags().Changed("message") {
			if editingCurrent {
				if err := s.SetTimerMessage(watson.PrimaryTimerID, message); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Updated message for running project %s.\n", styleText("project", frame.Project))
				return nil
			}
			updated, err := s.SetFrameMessage(frame.ID, message)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated message for frame %s.\n", styleText("id", shortID(updated.ID)))
			return nil
		}
		displayLoc := watsonDisplayLocation(s.Now())
		doc := map[string]any{"project": frame.Project, "start": time.Unix(frame.Start, 0).In(displayLoc).Format("2006-01-02 15:04:05"), "tags": frame.Tags, "message": frame.Message}
		if !editingCurrent {
			doc["stop"] = time.Unix(*frame.Stop, 0).In(displayLoc).Format("2006-01-02 15:04:05")
		}
		initial, err := marshalEditorJSON(doc)
		if err != nil {
			return err
		}
		input := cmd.InOrStdin()
		reader := bufio.NewReader(input)
		for {
			edited, changed, err := runEditor(initial, ".json", input, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if !changed || len(edited) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No change made.")
				return nil
			}
			startDefault := s.Now()
			stopDefault := s.Now()
			startDefault = time.Unix(frame.Start, 0).In(displayLoc)
			if !editingCurrent && frame.Stop != nil {
				stopDefault = time.Unix(*frame.Stop, 0).In(displayLoc)
			}
			parsed, err := parseEditDocumentOnDates(edited, !editingCurrent, s.Now(), startDefault, stopDefault)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error while parsing inputted values: %v\n", err)
				fmt.Fprintln(cmd.ErrOrStderr(), "Press any key to continue...")
				_, _ = bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				initial = edited
				continue
			}
			if cmd.Flags().Changed("message") {
				parsed.Message, parsed.MessageSet = message, true
			}
			if err := s.LoadConfig(); err != nil {
				return err
			}
			if s.Config.Bool("options", "confirm_new_project", false) || confirmProject {
				if err := confirmNewProject(reader, cmd, parsed.Project, s.Projects()); err != nil {
					return err
				}
			}
			if s.Config.Bool("options", "confirm_new_tag", false) || confirmTag {
				if err := confirmNewTags(reader, cmd, parsed.Tags, s.Tags()); err != nil {
					return err
				}
			}
			if editingCurrent {
				_, err = s.EditCurrent(parsed.Project, parsed.Tags, parsed.Start)
				if err != nil {
					return err
				}
				if parsed.MessageSet {
					if err := s.SetTimerMessage(watson.PrimaryTimerID, parsed.Message); err != nil {
						return err
					}
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Edited frame for project %s%s, from %s to - (-)\n", styleText("project", parsed.Project), formatTags(parsed.Tags), styleText("time", parsed.Start.Format("15:04:05")))
				return nil
			}
			updated, err := s.EditFrame(frame.ID, parsed.Project, parsed.Tags, parsed.Start, parsed.Stop)
			if err != nil {
				return err
			}
			if parsed.MessageSet {
				updated, err = s.SetFrameMessage(frame.ID, parsed.Message)
				if err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Edited frame for project %s%s, from %s to %s (%s)\n", styleText("project", updated.Project), formatTags(updated.Tags), styleText("time", parsed.Start.Format("15:04:05")), styleText("time", parsed.Stop.Format("15:04:05")), formatDuration(parsed.Stop.Unix()-parsed.Start.Unix()))
			return nil
		}
	}}
	cmd.Flags().BoolVarP(&confirmProject, "confirm-new-project", "c", false, "confirm addition of new project")
	cmd.Flags().BoolVarP(&confirmTag, "confirm-new-tag", "b", false, "confirm creation of new tag")
	cmd.Flags().StringVarP(&message, "message", "m", "", "replace or clear the session message")
	cmd.ValidArgsFunction = a.completeFrames
	return cmd
}

type parsedEdit struct {
	Project     string
	Tags        []string
	Start, Stop time.Time
	Message     string
	MessageSet  bool
}

func parseEditDocument(data []byte, needStop bool, now time.Time) (parsedEdit, error) {
	return parseEditDocumentOnDates(data, needStop, now, now, now)
}

func parseEditDocumentOnDates(data []byte, needStop bool, now, startDefault, stopDefault time.Time) (parsedEdit, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return parsedEdit{}, err
	}
	for _, key := range []string{"project", "start", "tags"} {
		if _, ok := raw[key]; !ok {
			return parsedEdit{}, errors.New("The edited frame must contain the project, start, and stop keys.")
		}
	}
	if needStop {
		if _, ok := raw["stop"]; !ok {
			return parsedEdit{}, errors.New("The edited frame must contain the project, start, and stop keys.")
		}
	}
	var doc editDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return parsedEdit{}, err
	}
	start, err := parseTime(doc.Start, startDefault)
	if err != nil {
		return parsedEdit{}, err
	}
	_, messageSet := raw["message"]
	result := parsedEdit{Project: doc.Project, Tags: doc.Tags, Start: start, Message: doc.Message, MessageSet: messageSet}
	if needStop {
		stop, err := parseTime(doc.Stop, stopDefault)
		if err != nil {
			return parsedEdit{}, err
		}
		result.Stop = stop
		if start.After(stop) {
			return parsedEdit{}, errors.New("Task cannot end before it starts.")
		}
		if stop.After(now) {
			return parsedEdit{}, errors.New("Stop time cannot be in the future")
		}
	}
	if start.After(now) {
		return parsedEdit{}, errors.New("Start time cannot be in the future")
	}
	return result, nil
}

func marshalEditorJSON(value any) ([]byte, error) {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetIndent("", "    ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

func runEditor(initial []byte, extension string, in io.Reader, out, errOut io.Writer) ([]byte, bool, error) {
	file, err := os.CreateTemp("", "burrowtime-*"+extension)
	if err != nil {
		return nil, false, err
	}
	path := file.Name()
	defer os.Remove(path)
	editorInitial := append([]byte(nil), initial...)
	if len(editorInitial) > 0 && !bytes.HasSuffix(editorInitial, []byte("\n")) {
		editorInitial = append(editorInitial, '\n')
	}
	diskInitial := editorInitial
	if runtime.GOOS == "windows" {
		diskInitial = bytes.ReplaceAll(diskInitial, []byte("\n"), []byte("\r\n"))
		diskInitial = append([]byte{0xef, 0xbb, 0xbf}, diskInitial...)
	}
	if _, err = file.Write(diskInitial); err != nil {
		file.Close()
		return nil, false, err
	}
	if err = file.Close(); err != nil {
		return nil, false, err
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		candidates := []string{"vim", "nano", "vi"}
		if runtime.GOOS == "windows" {
			candidates = []string{"notepad"}
		}
		for _, candidate := range candidates {
			if found, e := exec.LookPath(candidate); e == nil {
				editor = found
				break
			}
		}
	}
	if editor == "" {
		return nil, false, errors.New("no editor found; set VISUAL or EDITOR")
	}
	parts := splitCommandLine(editor)
	if len(parts) == 0 {
		return nil, false, errors.New("invalid editor command")
	}
	process := exec.Command(parts[0], append(parts[1:], path)...)
	process.Stdin = in
	process.Stdout = out
	process.Stderr = errOut
	if err := process.Run(); err != nil {
		return nil, false, err
	}
	edited, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	if runtime.GOOS == "windows" {
		edited = bytes.TrimPrefix(edited, []byte{0xef, 0xbb, 0xbf})
		edited = bytes.ReplaceAll(edited, []byte("\r\n"), []byte("\n"))
	}
	return edited, !bytes.Equal(edited, editorInitial), nil
}
