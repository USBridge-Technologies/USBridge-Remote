package gui

import "fyne.io/fyne/v2/container"

// tabSet is the main window's tab model: which of the four contents is
// selected, and the OnSelected hook. It replaces a container.AppTabs that was
// never shown (tabHeaderButtons and tabContentStack render the tabs): every
// AppTabs selection still built its hidden renderer and refreshed all four
// tabs' contents, re-measuring every text in them -- half a second per
// switch in the browser.
type tabSet struct {
	Items      []*container.TabItem
	OnSelected func(*container.TabItem)
	selected   int
}

func newTabSet(items ...*container.TabItem) *tabSet {
	return &tabSet{Items: items}
}

func (t *tabSet) SelectedIndex() int { return t.selected }

func (t *tabSet) Select(item *container.TabItem) {
	for i, it := range t.Items {
		if it == item {
			t.SelectIndex(i)
			return
		}
	}
}

// SelectIndex selects tab i and fires OnSelected, unless it already is the
// selected one (as AppTabs did).
func (t *tabSet) SelectIndex(i int) {
	if i < 0 || i >= len(t.Items) || i == t.selected {
		return
	}
	t.selected = i
	if t.OnSelected != nil {
		t.OnSelected(t.Items[i])
	}
}
