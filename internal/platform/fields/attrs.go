package fields

import "time"

type Attrs struct {
	keys []string
	vals map[string]any
}

func New() *Attrs {
	return &Attrs{
		keys: make([]string, 0, 10),
		vals: make(map[string]any),
	}
}

func (a *Attrs) Str(k string, v string) *Attrs {
	return a.Set(k, v)
}

func (a *Attrs) Bool(k string, v bool) *Attrs {
	return a.Set(k, v)
}

func (a *Attrs) Int(k string, v int) *Attrs {
	return a.Set(k, v)
}

func (a *Attrs) Int64(k string, v int64) *Attrs {
	return a.Set(k, v)
}

func (a *Attrs) Time(k string, v time.Time) *Attrs {
	return a.Set(k, v)
}

func (a *Attrs) Dur(k string, v time.Duration) *Attrs {
	return a.Set(k, v.String())
}

func (a *Attrs) DurMs(k string, v time.Duration) *Attrs {
	return a.Set(k, v.Milliseconds())
}

func (a *Attrs) Error(k string, v error) *Attrs {
	if v == nil {
		return a
	}
	return a.Set(k, v)
}

func (a *Attrs) Set(k string, v any) *Attrs {
	if _, ok := a.vals[k]; !ok {
		a.keys = append(a.keys, k)
	}
	a.vals[k] = v
	return a
}

func (a *Attrs) Merge(src *Attrs) *Attrs {
	if src == nil {
		return a
	}
	for _, k := range src.keys {
		a.Set(k, src.vals[k])
	}
	return a
}

func (a *Attrs) Args() []any {
	if a == nil || len(a.keys) == 0 {
		return nil
	}

	out := make([]any, 0, len(a.keys)*2)

	for _, k := range a.keys {
		v, ok := a.vals[k]
		if !ok {
			continue
		}
		out = append(out, k, v)
	}

	return out
}
