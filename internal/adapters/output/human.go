package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

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

// QueueListOutput wraps a QueueResult with pagination context.
type QueueListOutput struct {
	Result core.QueueResult
	Offset int64
	Count  int64
}

// SuggestShowOutput wraps raw suggest show JSON for human-readable rendering.
type SuggestShowOutput struct {
	Payload json.RawMessage
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
	case QueueListOutput:
		return renderQueueList(data)
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
	case core.LibraryRescanResult:
		return renderLibraryRescan(data)
	case LibraryItemsOutput:
		return renderLibraryItemsOutput(data)
	case SuggestShowOutput:
		return renderSuggestShow(data)
	case core.RawResult:
		return renderRaw(data)
	default:
		return "ok\n", nil
	}
}

func renderNodes(result core.NodesResult) (string, error) {
	if len(result.Nodes) == 0 {
		return "No nodes found. Is the broker running?\n", nil
	}
	nodes := append([]mu.Presence(nil), result.Nodes...)
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind < nodes[j].Kind
		}
		return nodes[i].Name < nodes[j].Name
	})
	rows := make([][]string, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, []string{node.Name, node.Kind, node.NodeID})
	}
	return Table{
		Columns: []Column{
			{Title: "NAME", Min: 12, Flex: 2},
			{Title: "KIND"},
			{Title: "NODE_ID", Style: Dim},
		},
		Rows: rows,
	}.Render(), nil
}

func renderStatus(result core.StatusResult) (string, error) {
	status := "unknown"
	var posMS, durMS int64
	volume := ""
	item := ""
	artistLine := ""
	owner := ""
	queue := ""

	if result.State.Playback != nil {
		status = result.State.Playback.Status
		posMS = result.State.Playback.PositionMS
		durMS = result.State.Playback.DurationMS
		volume = fmt.Sprintf("vol %d%%", int(result.State.Playback.Volume*100+0.5))
		if result.State.Playback.Mute {
			volume = "muted"
		}
	}
	if result.State.Current != nil {
		if d := result.State.Current.Display; d != nil {
			if d.Title != "" {
				item = d.Title
			}
			if d.Artist != "" {
				artistLine = d.Artist
			} else if len(d.Artists) > 0 {
				artistLine = strings.Join(d.Artists, ", ")
			}
		}
		if item == "" {
			if ref := result.State.Current.Ref; ref != nil {
				item = ref.ItemID
			} else if result.State.Current.QueueEntryID != "" {
				item = result.State.Current.QueueEntryID
			}
		}
	}
	if result.State.Queue != nil {
		queue = fmt.Sprintf("Queue: %d tracks (index %d) rev %d", result.State.Queue.Length, result.State.Queue.Index, result.State.Queue.Revision)
		if result.State.Queue.RepeatMode == "one" {
			queue += " repeat-one"
		} else if result.State.Queue.Repeat {
			queue += " repeat"
		}
	}
	if result.State.Session != nil {
		owner = fmt.Sprintf("owner %s", result.State.Session.Owner)
	}

	width := min(TerminalWidth(100), 100)

	// Header: playback glyph, renderer name, status.
	glyph := statusGlyph(status)
	header := fmt.Sprintf("%s %s %s", glyph,
		Bold(truncateCell(result.Renderer.Name, width-20)), styleStatus(status))
	lines := []string{strings.TrimRight(header, " ")}

	// Track line: title — artist.
	track := item
	if artistLine != "" && track != "" {
		track = fmt.Sprintf("%s %s %s", track, Dim("—"), Cyan(artistLine))
	} else if artistLine != "" {
		track = Cyan(artistLine)
	}
	if track != "" {
		lines = append(lines, "  "+track)
	}

	// Progress line: bar + position + volume.
	suffix := strings.TrimSpace(strings.TrimSpace(formatPosition(posMS, durMS)) + "  " + volume)
	if durMS > 0 && (status == "playing" || status == "paused") {
		barWidth := max(10, min(width-displayWidth(suffix)-6, 40))
		lines = append(lines, fmt.Sprintf("  %s %s", renderProgressBar(posMS, durMS, barWidth), Dim(suffix)))
	} else if suffix != "" {
		lines = append(lines, "  "+Dim(suffix))
	}

	// Queue / owner line.
	if info := strings.TrimSpace(strings.TrimSpace(queue) + "  " + owner); info != "" {
		lines = append(lines, "  "+Dim(info))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// statusGlyph maps playback status to its single-character indicator.
func statusGlyph(status string) string {
	switch strings.ToLower(status) {
	case "playing":
		return Green("▶")
	case "paused":
		return Yellow("⏸")
	case "stopped":
		return Red("■")
	default:
		return Dim("·")
	}
}

func renderSession(result core.SessionResult) (string, error) {
	expiresAt := time.Unix(result.Session.LeaseExpiresAt, 0)
	remaining := time.Until(expiresAt).Round(time.Second)
	return RenderDetails([][2]string{
		{"Renderer", result.RendererID},
		{"Session", result.Session.ID},
		{"Owner", result.Session.Owner},
		{"Expires", fmt.Sprintf("%s (in %s)", expiresAt.Format("15:04:05"), remaining)},
	}), nil
}

func renderQueue(result core.QueueResult) (string, error) {
	return renderQueueWithOffset(result, 0)
}

func renderQueueWithOffset(result core.QueueResult, offset int64) (string, error) {
	if len(result.Queue.Entries) == 0 {
		return "Queue is empty.\n", nil
	}
	cols := []Column{
		{Title: "#", Align: AlignRight},
		{Title: "TITLE", Min: 16, Flex: 3},
		{Title: "ARTIST", Min: 10, Flex: 2},
		{Title: "ALBUM", Min: 10, Flex: 2},
		{Title: "LEN", Align: AlignRight},
	}
	if result.FullIDs {
		cols = append(cols, Column{Title: "QUEUE_ID", Style: Dim}, Column{Title: "ITEM_ID", Style: Dim})
	}
	current := -1
	rows := make([][]string, 0, len(result.Queue.Entries))
	for idx, entry := range result.Queue.Entries {
		title, _, artist, album, length := displayFields(entry.Display)
		if title == "" {
			if entry.Ref != nil {
				title = entry.Ref.ItemID
			} else if entry.QueueEntryID != "" {
				title = entry.QueueEntryID
			}
		}
		absoluteIdx := offset + int64(idx)
		indexStr := fmt.Sprintf("%d", absoluteIdx)
		if absoluteIdx == result.Queue.Index {
			indexStr = "▸ " + indexStr
			current = idx
		}
		row := []string{indexStr, title, artist, album, length}
		if result.FullIDs {
			itemID := ""
			if entry.Ref != nil {
				itemID = entry.Ref.ItemID
			}
			row = append(row, entry.QueueEntryID, itemID)
		}
		rows = append(rows, row)
	}
	return Table{
		Columns: cols,
		Rows:    rows,
		RowStyle: func(i int) func(string) string {
			if i == current {
				return Green
			}
			return nil
		},
	}.Render(), nil
}

// displayFields extracts presentation fields from DisplayMetadata.
func displayFields(d *mu.DisplayMetadata) (title, mediaType, artist, album, length string) {
	if d == nil {
		return
	}
	title = d.Title
	mediaType = d.MediaType
	if d.Artist != "" {
		artist = d.Artist
	} else if len(d.Artists) > 0 {
		artist = strings.Join(d.Artists, ", ")
	}
	album = d.Album
	if d.DurationMS > 0 {
		length = formatMS(d.DurationMS)
	}
	return
}

func renderQueueList(data QueueListOutput) (string, error) {
	if len(data.Result.Queue.Entries) == 0 {
		return "Queue is empty.\n", nil
	}
	table, err := renderQueueWithOffset(data.Result, data.Offset)
	if err != nil {
		return "", err
	}
	numEntries := int64(len(data.Result.Queue.Entries))
	if data.Offset > 0 || numEntries == data.Count {
		end := data.Offset + numEntries
		line := fmt.Sprintf("%d\u2013%d", data.Offset+1, end)
		if numEntries == data.Count {
			line += fmt.Sprintf(" \u00b7 --offset %d for more", end)
		}
		table += Dim(line) + "\n"
	}
	return table, nil
}

func renderQueueNow(result core.QueueNowResult) (string, error) {
	if result.Current == nil {
		return "(none)\n", nil
	}
	item := formatItem(result.Current)
	return fmt.Sprintf("%s\n", item), nil
}

func renderPlaylists(result core.PlaylistListResult) (string, error) {
	if len(result.Playlists) == 0 {
		return "No playlists found.\n", nil
	}
	rows := make([][]string, 0, len(result.Playlists))
	for _, pl := range result.Playlists {
		rows = append(rows, []string{pl.Name, fmt.Sprintf("%d", pl.Revision), pl.PlaylistID})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return Table{
		Columns: []Column{
			{Title: "NAME", Min: 12, Flex: 1},
			{Title: "REV", Align: AlignRight},
			{Title: "PLAYLIST_ID", Style: Dim},
		},
		Rows: rows,
	}.Render(), nil
}

func renderPlaylistShow(result core.PlaylistShowResult) (string, error) {
	if len(result.Entries) == 0 {
		return fmt.Sprintf("Playlist: %s (0 tracks)\n", result.Name), nil
	}
	header := fmt.Sprintf("Playlist: %s (%d tracks)\n\n", result.Name, len(result.Entries))

	cols := []Column{
		{Title: "#", Align: AlignRight},
		{Title: "TITLE", Min: 16, Flex: 3},
		{Title: "ARTIST", Min: 10, Flex: 2},
		{Title: "ALBUM", Min: 10, Flex: 2},
		{Title: "LEN", Align: AlignRight},
	}
	if result.FullIDs {
		cols = append(cols, Column{Title: "ENTRY_ID", Style: Dim}, Column{Title: "ITEM_ID", Style: Dim})
	}
	rows := make([][]string, 0, len(result.Entries))
	for idx, entry := range result.Entries {
		title, _, artist, album, length := displayFields(entry.Display)
		itemID := ""
		if entry.Ref != nil {
			itemID = entry.Ref.ItemID
		}
		if title == "" {
			if itemID != "" {
				title = itemID
			} else if entry.EntryID != "" {
				title = entry.EntryID
			}
		}
		row := []string{fmt.Sprintf("%d", idx), title, artist, album, length}
		if result.FullIDs {
			row = append(row, entry.EntryID, itemID)
		}
		rows = append(rows, row)
	}
	return header + Table{Columns: cols, Rows: rows}.Render(), nil
}

func renderSnapshots(result core.SnapshotListResult) (string, error) {
	if len(result.Snapshots) == 0 {
		return "No snapshots found.\n", nil
	}
	rows := make([][]string, 0, len(result.Snapshots))
	for _, snap := range result.Snapshots {
		rows = append(rows, []string{snap.Name, fmt.Sprintf("%d", snap.Revision), snap.SnapshotID})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return Table{
		Columns: []Column{
			{Title: "NAME", Min: 12, Flex: 1},
			{Title: "REV", Align: AlignRight},
			{Title: "SNAPSHOT_ID", Style: Dim},
		},
		Rows: rows,
	}.Render(), nil
}

func renderSuggestions(result core.SuggestListResult) (string, error) {
	if len(result.Suggestions) == 0 {
		return "No suggestions available.\n", nil
	}
	rows := make([][]string, 0, len(result.Suggestions))
	for _, sug := range result.Suggestions {
		rows = append(rows, []string{sug.Name, fmt.Sprintf("%d", sug.Revision), sug.SuggestionID})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return Table{
		Columns: []Column{
			{Title: "NAME", Min: 12, Flex: 1},
			{Title: "REV", Align: AlignRight},
			{Title: "SUGGESTION_ID", Style: Dim},
		},
		Rows: rows,
	}.Render(), nil
}

func renderSuggestShow(data SuggestShowOutput) (string, error) {
	var suggestion struct {
		SuggestionID string `json:"suggestionId"`
		Name         string `json:"name"`
		Entries      []struct {
			EntryID string              `json:"entryId,omitempty"`
			Ref     *mu.LibraryItemRef  `json:"ref,omitempty"`
			Display *mu.DisplayMetadata `json:"display,omitempty"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data.Payload, &suggestion); err != nil {
		// Fall back to raw JSON if we can't parse
		var buf bytes.Buffer
		if err := json.Indent(&buf, data.Payload, "", "  "); err != nil {
			return string(data.Payload), nil
		}
		return buf.String() + "\n", nil
	}

	header := fmt.Sprintf("Suggestion: %s (%d tracks)\n\n", suggestion.Name, len(suggestion.Entries))

	if len(suggestion.Entries) == 0 {
		return header + "No tracks.\n", nil
	}

	rows := make([][]string, 0, len(suggestion.Entries))
	for idx, entry := range suggestion.Entries {
		title, _, artist, album, length := displayFields(entry.Display)
		if title == "" {
			if entry.Ref != nil {
				title = entry.Ref.ItemID
			} else if entry.EntryID != "" {
				title = entry.EntryID
			}
		}
		rows = append(rows, []string{fmt.Sprintf("%d", idx), title, artist, album, length})
	}
	return header + Table{
		Columns: []Column{
			{Title: "#", Align: AlignRight},
			{Title: "TITLE", Min: 16, Flex: 3},
			{Title: "ARTIST", Min: 10, Flex: 2},
			{Title: "ALBUM", Min: 10, Flex: 2},
			{Title: "LEN", Align: AlignRight},
		},
		Rows: rows,
	}.Render(), nil
}

func renderLibraryResolve(result core.LibraryResolveResult) (string, error) {
	title := result.Item.Ref.ItemID
	if d := result.Item.Display; d != nil && d.Title != "" {
		title = d.Title
	}
	pairs := [][2]string{{"Item", Bold(title)}}
	if d := result.Item.Display; d != nil {
		artist := d.Artist
		if artist == "" && len(d.Artists) > 0 {
			artist = strings.Join(d.Artists, ", ")
		}
		pairs = append(pairs,
			[2]string{"Artist", artist},
			[2]string{"Album", d.Album})
		if d.DurationMS > 0 {
			pairs = append(pairs, [2]string{"Duration", formatMS(d.DurationMS)})
		}
	}
	pairs = append(pairs, [2]string{"Item ID", Dim(result.Item.Ref.ItemID)})

	var buf strings.Builder
	buf.WriteString(RenderDetails(pairs))
	switch {
	case result.Sources == nil:
		buf.WriteString(Dim("Sources not requested; pass --sources for playable URLs.") + "\n")
	case len(result.Sources.Sources) == 0:
		buf.WriteString("Sources: (none)\n")
	default:
		buf.WriteString(fmt.Sprintf("Sources (%d):\n", len(result.Sources.Sources)))
		for _, src := range result.Sources.Sources {
			mime := src.Mime
			if mime == "" {
				mime = "unknown"
			}
			buf.WriteString(fmt.Sprintf("  %s %s\n", src.URL, Dim("("+mime+")")))
		}
	}
	return buf.String(), nil
}

func renderLibraryRescan(result core.LibraryRescanResult) (string, error) {
	if result.Status == "started" {
		return fmt.Sprintf("rescan started: %s\n", result.Message), nil
	}
	return fmt.Sprintf("rescan complete: %d items indexed\n", result.Items), nil
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
	DurationMS  int64    `json:"durationMs,omitempty"`
}

func renderLibraryItemsOutput(result LibraryItemsOutput) (string, error) {
	var payload libraryItemsReply
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		return "", err
	}
	if len(payload.Items) == 0 {
		return Dim("No items found.") + "\n", nil
	}
	rows := make([][]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		length := ""
		if item.DurationMS > 0 {
			length = formatMS(item.DurationMS)
		}
		rows = append(rows, []string{
			item.Name,
			item.Type,
			strings.Join(item.Artists, ", "),
			item.Album,
			length,
			item.ItemID,
		})
	}
	table := Table{
		Columns: []Column{
			{Title: "NAME", Min: 16, Flex: 3},
			{Title: "TYPE"},
			{Title: "ARTIST", Min: 10, Flex: 2},
			{Title: "ALBUM", Min: 10, Flex: 2},
			{Title: "LEN", Align: AlignRight},
			{Title: "ITEM_ID", Style: Dim},
		},
		Rows: rows,
	}.Render()
	table += Footer(payload.Start, int64(len(payload.Items)), payload.Total)
	return table, nil
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

func renderProgressBar(pos, dur int64, width int) string {
	if dur <= 0 || width < 5 {
		return ""
	}
	barWidth := max(width, 3)
	fraction := float64(pos) / float64(dur)
	fraction = max(0, min(1, fraction))
	filled := int(fraction * float64(barWidth))
	filledPart := strings.Repeat("━", filled)
	cursor := ""
	rest := barWidth - filled
	if rest > 0 {
		cursor = "╸"
		rest--
	}
	return Green(filledPart) + cursor + Dim(strings.Repeat("─", rest))
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
	hours := secs / 3600
	mins := (secs % 3600) / 60
	sec := secs % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, sec)
	}
	return fmt.Sprintf("%d:%02d", mins, sec)
}

func formatItem(current *mu.CurrentItemState) string {
	if d := current.Display; d != nil {
		title := d.Title
		artist := d.Artist
		if artist == "" && len(d.Artists) > 0 {
			artist = strings.Join(d.Artists, ", ")
		}
		if title != "" && artist != "" {
			return fmt.Sprintf("%s - %s", artist, title)
		}
		if title != "" {
			return title
		}
	}
	if current.Ref != nil {
		return current.Ref.ItemID
	}
	return current.QueueEntryID
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

func styleStatus(status string) string {
	switch strings.ToLower(status) {
	case "playing":
		return Green(status)
	case "paused":
		return Yellow(status)
	case "stopped":
		return Red(status)
	default:
		return Dim(status)
	}
}

func displayWidth(value string) int {
	return runewidth.StringWidth(value)
}

func truncateCell(value string, max int) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	if max <= 0 {
		return value
	}
	if runewidth.StringWidth(value) <= max {
		return value
	}
	ellipsis := "\u2026" // single-char ellipsis; width varies under East Asian rules
	ellW := runewidth.StringWidth(ellipsis)
	if max <= ellW {
		return truncateByWidth(value, max)
	}
	return truncateByWidth(value, max-ellW) + ellipsis
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
