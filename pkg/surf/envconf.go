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

	OnErr func(error)
}

type variable struct {
	name  string
	value string
	desc  string
	typ   string
}

// NewEnvConf returns configuration instance that use environment variable for
// configuration.
func NewEnvConf() *EnvConf {
	c := &EnvConf{
		env: make(map[string]string),
		OnErr: func(err error) {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		},
	}
	for _, raw := range os.Environ() {
		pair := strings.SplitN(raw, "=", 2)
		c.env[pair[0]] = pair[1]
	}
	return c
}

// WriteHelp write information about used environment variables, their value in
// current environment and description if provided.
func (c *EnvConf) WriteHelp(w io.Writer) {
	wr := tabwriter.NewWriter(w, 1, 2, 1, ' ', tabwriter.TabIndent)
	defer wr.Flush()
	for _, v := range c.vars {
		io.WriteString(wr, v.name)
		io.WriteString(wr, "\t")
		io.WriteString(wr, v.typ)
		io.WriteString(wr, "\t")
		io.WriteString(wr, v.value)
		if len(v.desc) > 0 {
			io.WriteString(wr, "\t")
			io.WriteString(wr, v.desc)
		}
		io.WriteString(wr, "\n")
	}
}

func (c *EnvConf) Str(name, fallback, description string) string {
	value := fallback
	if v, ok := c.env[name]; ok {
		value = v
	}
	c.vars = append(c.vars, variable{
		name:  name,
		value: value,
		desc:  description,
		typ:   "string",
	})
	return value
}

func (c *EnvConf) Secret(name, fallback, description string) string {
	value := fallback
	if v, ok := c.env[name]; ok {
		value = v
	}

	pubvalue := "[..]" // secret does not expose it's default value
	if len(value) == 0 {
		pubvalue = ""
	}
	c.vars = append(c.vars, variable{
		name:  name,
		value: pubvalue,
		desc:  description,
		typ:   "string",
	})
	return value
}

func (c *EnvConf) Int(name string, fallback int, description string) int {
	value := fallback
	if v, ok := c.env[name]; ok {
		if n, err := strconv.Atoi(v); err != nil {
			c.OnErr(fmt.Errorf("%s: cannot parse integer: %s", name, err))
		} else {
			value = n
		}
	}

	c.vars = append(c.vars, variable{
		name:  name,
		value: strconv.Itoa(value),
		desc:  description,
		typ:   "int",
	})
	return value
}

func (c *EnvConf) Bool(name string, fallback bool, description string) bool {
	value := fallback
	if v, ok := c.env[name]; ok {
		if n, err := strconv.ParseBool(v); err != nil {
			c.OnErr(fmt.Errorf("%s: cannot parse boolean: %s", name, err))
		} else {
			value = n
		}
	}
	c.vars = append(c.vars, variable{
		name:  name,
		value: fmt.Sprint(value),
		desc:  description,
		typ:   "bool",
	})

	return value
}

func (c *EnvConf) Duration(name string, fallback time.Duration, description string) time.Duration {
	value := fallback
	if v, ok := c.env[name]; ok {
		if d, err := time.ParseDuration(v); err != nil {
			c.OnErr(fmt.Errorf("%s: cannot parse duration: %s", name, err))
		} else {
			value = d
		}
	}
	c.vars = append(c.vars, variable{
		name:  name,
		value: value.String(),
		desc:  description,
		typ:   "duration",
	})

	return value
}
