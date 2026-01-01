package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/pterm/pterm"

	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// HumanPrinter prints human-readable output.
type HumanPrinter struct{}

func init() {
	runewidth.DefaultCondition.EastAsianWidth = true
}

// LibraryItemsOutput carries library browse/search payloads with context.
type LibraryItemsOutput struct {
	LibraryID string
	Payload   json.RawMessage
}

// Render returns human output as a string.
func (HumanPrinter) Render(v any) (string, error) {
	return renderHuman(v)
}

// Print renders human output.
func (HumanPrinter) Print(v any) error {
	out, err := renderHuman(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, out)
	return err
}

func renderHuman(v any) (string, error) {
	switch data := v.(type) {
	case core.NodesResult:
		return renderNodes(data)
	case core.StatusResult:
		return renderStatus(data)
	case core.SessionResult:
		return renderSession(data)
	case core.QueueResult:
		return renderQueue(data)
	case core.QueueNowResult:
		return renderQueueNow(data)
	case core.PlaylistListResult:
		return renderPlaylists(data)
	case core.PlaylistShowResult:
		return renderPlaylistShow(data)
	case core.SnapshotListResult:
		return renderSnapshots(data)
	case core.SuggestListResult:
		return renderSuggestions(data)
	case core.LibraryResolveResult:
		return renderLibraryResolve(data)
	case LibraryItemsOutput:
		return renderLibraryItemsOutput(data)
	case core.RawResult:
		return renderRaw(data)
	default:
		return "ok\n", nil
	}
}

func renderNodes(result core.NodesResult) (string, error) {
	rows := make([][]string, 0, len(result.Nodes))
	for _, node := range result.Nodes {
		rows = append(rows, []string{node.Name, node.Kind, node.NodeID})
	}
	return renderTable([]string{"NAME", "KIND", "NODE_ID"}, rows)
}

func renderStatus(result core.StatusResult) (string, error) {
	status := "unknown"
	position := ""
	volume := ""
	item := ""
	artistLine := ""
	owner := ""
	queue := ""

	if result.State.Playback != nil {
		status = result.State.Playback.Status
		position = formatPosition(result.State.Playback.PositionMS, result.State.Playback.DurationMS)
		volume = fmt.Sprintf("vol %d%%", int(result.State.Playback.Volume*100+0.5))
		if result.State.Playback.Mute {
			volume = "muted"
		}
	}
	if result.State.Current != nil {
		if result.State.Current.Metadata != nil {
			title, _ := result.State.Current.Metadata["title"].(string)
			artist, _ := result.State.Current.Metadata["artist"].(string)
			if title != "" {
				item = title
			}
			if artist != "" {
				artistLine = artist
			}
		}
		if item == "" {
			item = result.State.Current.ItemID
		}
	}
	if result.State.Queue != nil {
		queue = fmt.Sprintf("Queue: %d tracks (index %d) rev %d", result.State.Queue.Length, result.State.Queue.Index, result.State.Queue.Revision)
		if result.State.Queue.RepeatMode == "one" {
			queue = queue + " repeat-one"
		} else if result.State.Queue.Repeat {
			queue = queue + " repeat"
		}
	}
	if result.State.Session != nil {
		owner = fmt.Sprintf("owner %s", result.State.Session.Owner)
	}

	statusStyled := styleStatus(status)
	innerWidth := pterm.GetTerminalWidth() - 4
	if innerWidth < 20 {
		innerWidth = 80
	}
	titleName := truncateCell(result.Renderer.Name, innerWidth-10)
	title := fmt.Sprintf("%s [%s]", titleName, statusStyled)
	suffix := strings.TrimSpace(fmt.Sprintf("%s  %s", position, volume))
	itemMax := innerWidth
	if suffix != "" {
		itemMax = innerWidth - displayWidth(suffix) - 2
		if itemMax < 10 {
			itemMax = 10
		}
	}
	if itemMax > 60 {
		itemMax = 60
	}
	item = truncateCell(item, itemMax)
	if displayWidth(item) > itemMax {
		item = truncateByWidth(item, itemMax)
	}
	line := strings.TrimSpace(item)
	if suffix != "" {
		line = strings.TrimSpace(fmt.Sprintf("%s  %s", item, suffix))
	}
	lines := []string{truncateCell(line, innerWidth)}
	if artistLine != "" {
		label := fmt.Sprintf("Artist: %s", artistLine)
		label = truncateCell(label, itemMax)
		if displayWidth(label) > itemMax {
			label = truncateByWidth(label, itemMax)
		}
		lines = append(lines, pterm.FgCyan.Sprint(label))
	}
	if queue != "" || owner != "" {
		infoLine := strings.TrimSpace(fmt.Sprintf("%s %s", queue, owner))
		lines = append(lines, truncateCell(infoLine, innerWidth))
	}
	box := pterm.DefaultBox.WithTitle(title)
	return box.Sprint(strings.Join(lines, "\n")), nil
}

func renderSession(result core.SessionResult) (string, error) {
	expires := time.Unix(result.Session.LeaseExpiresAt, 0).Format(time.RFC3339)
	return fmt.Sprintf("session %s expires %s\n", result.Session.ID, expires), nil
}

func renderQueue(result core.QueueResult) (string, error) {
	headers := []string{"INDEX", "TITLE", "TYPE", "ARTIST", "ALBUM", "LEN"}
	if result.FullIDs {
		headers = append(headers, "QUEUE_ID", "ITEM_ID")
	}
	rows := make([][]string, 0, len(result.Queue.Entries))
	for idx, entry := range result.Queue.Entries {
		title := entry.ItemID
		typ := ""
		artist := ""
		album := ""
		length := ""
		if entry.Metadata != nil {
			if val, ok := entry.Metadata["title"].(string); ok && val != "" {
				title = val
			}
			if val, ok := entry.Metadata["mediaType"].(string); ok && val != "" {
				typ = val
			} else if val, ok := entry.Metadata["type"].(string); ok {
				typ = val
			}
			if val, ok := entry.Metadata["artist"].(string); ok {
				artist = val
			}
			if val, ok := entry.Metadata["album"].(string); ok {
				album = val
			}
			length = formatDuration(entry.Metadata["durationMs"])
		}
		row := []string{
			fmt.Sprintf("%d", idx),
			truncateCell(title, 64),
			truncateCell(typ, 16),
			truncateCell(artist, 32),
			truncateCell(album, 40),
			length,
		}
		if result.FullIDs {
			row = append(row, entry.QueueEntryID, entry.ItemID)
		}
		rows = append(rows, row)
	}
	return renderTable(headers, rows)
}

func renderQueueNow(result core.QueueNowResult) (string, error) {
	if result.Current == nil {
		return "(none)\n", nil
	}
	item := formatItem(result.Current)
	return fmt.Sprintf("%s\n", item), nil
}

func renderPlaylists(result core.PlaylistListResult) (string, error) {
	rows := make([][]string, 0, len(result.Playlists))
	for _, pl := range result.Playlists {
		rows = append(rows, []string{pl.Name, pl.PlaylistID, fmt.Sprintf("%d", pl.Revision)})
	}
	return renderTable([]string{"NAME", "PLAYLIST_ID", "REVISION"}, rows)
}

func renderPlaylistShow(result core.PlaylistShowResult) (string, error) {
	headers := []string{"INDEX", "TITLE", "TYPE", "ARTIST", "ALBUM", "LEN"}
	if result.FullIDs {
		headers = append(headers, "ENTRY_ID", "ITEM_ID")
	}
	rows := make([][]string, 0, len(result.Entries))
	for idx, entry := range result.Entries {
		title := entry.ItemID
		typ := ""
		artist := ""
		album := ""
		length := ""
		if entry.Metadata != nil {
			if val, ok := entry.Metadata["title"].(string); ok && val != "" {
				title = val
			}
			if val, ok := entry.Metadata["mediaType"].(string); ok && val != "" {
				typ = val
			} else if val, ok := entry.Metadata["type"].(string); ok {
				typ = val
			}
			if val, ok := entry.Metadata["artist"].(string); ok {
				artist = val
			}
			if val, ok := entry.Metadata["album"].(string); ok {
				album = val
			}
			length = formatDuration(entry.Metadata["durationMs"])
		}
		row := []string{
			fmt.Sprintf("%d", idx),
			truncateCell(title, 64),
			truncateCell(typ, 16),
			truncateCell(artist, 32),
			truncateCell(album, 40),
			length,
		}
		if result.FullIDs {
			row = append(row, entry.EntryID, entry.ItemID)
		}
		rows = append(rows, row)
	}
	return renderTable(headers, rows)
}

func renderSnapshots(result core.SnapshotListResult) (string, error) {
	rows := make([][]string, 0, len(result.Snapshots))
	for _, snap := range result.Snapshots {
		rows = append(rows, []string{snap.Name, snap.SnapshotID, fmt.Sprintf("%d", snap.Revision)})
	}
	return renderTable([]string{"NAME", "SNAPSHOT_ID", "REVISION"}, rows)
}

func renderSuggestions(result core.SuggestListResult) (string, error) {
	rows := make([][]string, 0, len(result.Suggestions))
	for _, sug := range result.Suggestions {
		rows = append(rows, []string{sug.Name, sug.SuggestionID, fmt.Sprintf("%d", sug.Revision)})
	}
	return renderTable([]string{"NAME", "SUGGESTION_ID", "REVISION"}, rows)
}

func renderLibraryResolve(result core.LibraryResolveResult) (string, error) {
	if len(result.Item.Sources) == 0 {
		return "no sources\n", nil
	}
	rows := make([][]string, 0, len(result.Item.Sources))
	for _, src := range result.Item.Sources {
		rows = append(rows, []string{src.URL, src.Mime})
	}
	return renderTable([]string{"URL", "MIME"}, rows)
}

func renderRaw(result core.RawResult) (string, error) {
	raw, err := rawBytes(result.Data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n", string(raw)), nil
}

type libraryItemsReply struct {
	Items []libraryItem `json:"items"`
	Start int64         `json:"start"`
	Count int64         `json:"count"`
	Total int64         `json:"total"`
}

type libraryItem struct {
	ItemID      string   `json:"itemId"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	MediaType   string   `json:"mediaType"`
	Artists     []string `json:"artists,omitempty"`
	Album       string   `json:"album,omitempty"`
	ContainerID string   `json:"containerId,omitempty"`
}

func renderLibraryItemsOutput(result LibraryItemsOutput) (string, error) {
	var payload libraryItemsReply
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		return "", err
	}
	rows := make([][]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		artist := strings.Join(item.Artists, ", ")
		libRef := fmt.Sprintf("lib:%s:%s", result.LibraryID, item.ItemID)
		rows = append(rows, []string{
			truncateCell(item.Name, 64),
			truncateCell(item.Type, 16),
			truncateCell(artist, 32),
			truncateCell(item.Album, 40),
			truncateCell(item.ContainerID, 36),
			item.ItemID,
			libRef,
		})
	}
	return renderTable([]string{"NAME", "TYPE", "ARTIST", "ALBUM", "CONTAINER_ID", "ITEM_ID", "LIB_REF"}, rows)
}

func rawBytes(data any) ([]byte, error) {
	switch val := data.(type) {
	case json.RawMessage:
		return val, nil
	case []byte:
		return val, nil
	default:
		out, err := json.Marshal(val)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

func formatPosition(pos, dur int64) string {
	if pos == 0 && dur == 0 {
		return ""
	}
	if dur > 0 {
		percent := int64(0)
		if dur > 0 {
			percent = (pos * 100) / dur
		}
		return fmt.Sprintf("%s / %s (%d%%)", formatMS(pos), formatMS(dur), percent)
	}
	return fmt.Sprintf("%s / %s", formatMS(pos), formatMS(dur))
}

func formatMS(ms int64) string {
	if ms <= 0 {
		return "0:00"
	}
	secs := ms / 1000
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

func formatItem(current *mu.CurrentItemState) string {
	if current.Metadata != nil {
		title, _ := current.Metadata["title"].(string)
		artist, _ := current.Metadata["artist"].(string)
		if title != "" && artist != "" {
			return fmt.Sprintf("%s - %s", artist, title)
		}
		if title != "" {
			return title
		}
	}
	return current.ItemID
}

func formatDuration(value any) string {
	switch v := value.(type) {
	case int64:
		return formatMS(v)
	case int:
		return formatMS(int64(v))
	case float64:
		return formatMS(int64(v))
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return formatMS(parsed)
		}
	}
	return ""
}

func renderTable(headers []string, rows [][]string) (string, error) {
	data := pterm.TableData{headers}
	data = append(data, rows...)
	table := pterm.DefaultTable.WithHasHeader(true).WithData(data)
	return table.Srender()
}

func styleStatus(status string) string {
	switch strings.ToLower(status) {
	case "playing":
		return pterm.FgGreen.Sprint(status)
	case "paused":
		return pterm.FgYellow.Sprint(status)
	case "stopped":
		return pterm.FgRed.Sprint(status)
	default:
		return pterm.FgGray.Sprint(status)
	}
}

func displayWidth(value string) int {
	return runewidth.StringWidth(value)
}

func truncateCell(value string, max int) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "|", "/")
	if max <= 0 {
		return value
	}
	if runewidth.StringWidth(value) <= max {
		return value
	}
	if max <= 3 {
		return truncateByWidth(value, max)
	}
	return truncateByWidth(value, max-3) + "..."
}

func truncateByWidth(value string, max int) string {
	if max <= 0 {
		return ""
	}
	width := 0
	var out strings.Builder
	for _, r := range value {
		rw := runewidth.RuneWidth(r)
		if width+rw > max {
			break
		}
		out.WriteRune(r)
		width += rw
	}
	return out.String()
}
