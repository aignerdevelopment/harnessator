package config

// Scope-layer provenance accessors (see layers.go): resolution annotates
// "why" output with the layer that introduced an entry, so "who enabled
// this" stays auditable across the corporate/team/project/personal/local
// merge.

func (p *ProjectConfig) setProvenance(prov map[string]string) { p.prov = prov }

// Origin returns the scope layer that introduced a config entry. Keys:
// "template\x00<ref>", "include\x00<cat>\x00<entry>",
// "exclude\x00<cat>\x00<entry>". Empty for standalone-loaded configs
// and unseen entries.
func (p *ProjectConfig) Origin(key string) string {
	if p == nil || p.prov == nil {
		return ""
	}
	return p.prov[key]
}

// EnsureSelect returns the Selection for a category (zero value when
// absent). Map values are not addressable, so callers mutate the copy
// and store it back via PutSelect.
func (p *ProjectConfig) EnsureSelect(cat string) Selection {
	return p.Select[cat]
}

// PutSelect stores a Selection for a category.
func (p *ProjectConfig) PutSelect(cat string, s Selection) {
	if p.Select == nil {
		p.Select = map[string]Selection{}
	}
	p.Select[cat] = s
}
