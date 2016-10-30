package main

import "strconv"

type Caps map[string]interface{}

func (c *Caps) capability(k string) string {
	dc := (*c)["desiredCapabilities"]
	switch dc.(type) {
	case map[string]interface{}:
		v := dc.(map[string]interface{})
		switch v[k].(type) {
		case string:
			return v[k].(string)
		}
	}
	return ""
}

func (c *Caps) processName() string {
	return c.capability("processName")
}

func (c *Caps) processPriority() int {
	val, err := strconv.Atoi(c.capability("processPriority"))
	if err != nil {
		val = 1
	}
	return val

}

func (c *Caps) browser() string {
	return c.capability("browserName")
}

func (c *Caps) version() string {
	return c.capability("version")
}
