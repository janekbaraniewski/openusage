package core

type DetailSectionStyle string

const (
	DetailSectionStyleUsage    DetailSectionStyle = "usage"
	DetailSectionStyleSpending DetailSectionStyle = "spending"
	DetailSectionStyleTokens   DetailSectionStyle = "tokens"
	DetailSectionStyleActivity DetailSectionStyle = "activity"
	DetailSectionStyleList     DetailSectionStyle = "list"
)

type DetailSection struct {
	Name  string
	Order int
	Style DetailSectionStyle
}

type DetailWidget struct {
	Sections []DetailSection
}

func DefaultDetailWidget() DetailWidget {
	return DetailWidget{
		Sections: []DetailSection{
			{Name: "Usage", Order: 1, Style: DetailSectionStyleUsage},
			{Name: "Spending", Order: 2, Style: DetailSectionStyleSpending},
			{Name: "Tokens", Order: 3, Style: DetailSectionStyleTokens},
			{Name: "Activity", Order: 4, Style: DetailSectionStyleActivity},
		},
	}
}

func (w DetailWidget) section(name string) (DetailSection, bool) {
	for _, s := range w.Sections {
		if s.Name == name {
			return s, true
		}
	}
	return DetailSection{}, false
}

func (w DetailWidget) SectionOrder(name string) int {
	if s, ok := w.section(name); ok && s.Order > 0 {
		return s.Order
	}
	return 0
}

func (w DetailWidget) SectionStyle(name string) DetailSectionStyle {
	if s, ok := w.section(name); ok && s.Style != "" {
		return s.Style
	}
	return DetailSectionStyleList
}
