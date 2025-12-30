package core

// Config is runtime configuration for the CLI.
type Config struct {
	Broker    string
	Identity  string
	TopicBase string
	Aliases   map[string]string
	Defaults  Defaults
}

// Defaults defines default selector values.
type Defaults struct {
	Renderer       string
	PlaylistServer string
	Library        string
}
