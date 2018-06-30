package surf

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type EnvConf struct {
	env  map[string]string
	vars []variable

	onerr func(error)
}

type variable struct {
	name     string
	fallback string
	desc     string
}

// NewEnvConf returns configuration instance that use environment variable for
// configuration.
func NewEnvConf() *EnvConf {
	c := &EnvConf{
		env:   make(map[string]string),
		onerr: exitOnError,
	}
	for _, raw := range os.Environ() {
		pair := strings.SplitN(raw, "=", 2)
		c.env[pair[0]] = pair[1]
	}
	return c
}

func exitOnError(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(2)
}

func (c *EnvConf) PrintHelp() {
	fmt.Fprintln(os.Stderr, "Supported environment variables:")
	w := tabwriter.NewWriter(os.Stderr, 1, 2, 1, ' ', tabwriter.TabIndent)
	defer w.Flush()
	for _, v := range c.vars {
		io.WriteString(w, v.name)
		io.WriteString(w, "\t")
		io.WriteString(w, v.fallback)
		if len(v.desc) > 0 {
			io.WriteString(w, "\t")
			io.WriteString(w, v.desc)
		}
		io.WriteString(w, "\n")
	}
}

func (c *EnvConf) Str(name, fallback, description string) string {
	c.vars = append(c.vars, variable{
		name:     name,
		fallback: fallback,
		desc:     description,
	})
	if v, ok := c.env[name]; ok {
		return v
	}
	return fallback
}

func (c *EnvConf) Secret(name, fallback, description string) string {
	c.vars = append(c.vars, variable{
		name:     name,
		fallback: "<secret>", // secret does not expose it's default value
		desc:     description,
	})
	if v, ok := c.env[name]; ok {
		return v
	}
	return fallback
}

func (c *EnvConf) Int(name string, fallback int, description string) int {
	c.vars = append(c.vars, variable{
		name:     name,
		fallback: strconv.Itoa(fallback),
		desc:     description,
	})

	if v, ok := c.env[name]; ok {
		if n, err := strconv.Atoi(v); err != nil {
			c.onerr(fmt.Errorf("%s: cannot parse integer: %s", name, err))
		} else {
			return n
		}
	}
	return fallback
}

func (c *EnvConf) Bool(name string, fallback bool, description string) bool {
	c.vars = append(c.vars, variable{
		name:     name,
		fallback: fmt.Sprint(fallback),
		desc:     description,
	})

	if v, ok := c.env[name]; ok {
		if n, err := strconv.ParseBool(v); err != nil {
			c.onerr(fmt.Errorf("%s: cannot parse boolean: %s", name, err))
		} else {
			return n
		}
	}
	return fallback
}

func (c *EnvConf) Duration(name string, fallback time.Duration, description string) time.Duration {
	c.vars = append(c.vars, variable{
		name:     name,
		fallback: fallback.String(),
		desc:     description,
	})

	if v, ok := c.env[name]; ok {
		if d, err := time.ParseDuration(v); err != nil {
			c.onerr(fmt.Errorf("%s: cannot parse duration: %s", name, err))
		} else {
			return d
		}
	}
	return fallback
}
