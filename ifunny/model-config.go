package ifunny

type IFunnyConfig struct {
	BearerToken string `cty:"bearer-token"`
	UserAgent   string `cty:"user-agent"`
	APIRoot     string `cty:"api-root"`

	Feed        string `cty:"feed"`
	StopAfter   int    `cty:"stop-after"`
	PerMinute   int    `cty:"per-minute"`
	ExitOnError bool   `cty:"exit-on-error"`
}

func mustConfig(parse func(interface{}) error) *IFunnyConfig {
	config := new(IFunnyConfig)
	if err := parse(config); err != nil {
		panic(err)
	}

	return config
}
