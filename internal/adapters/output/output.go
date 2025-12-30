package output

// Printer renders output to stdout.
type Printer interface {
	Print(v any) error
}
