package main

import (
	"github.com/egoist/mygo/ui"
	"net/mail"
	"strings"
)

func (a *app) contactsSettings(c *ui.Context) {
	section(c, "People you work with")
	note(c, "Recipients from your mail are available as suggestions in the composer. Add a contact here to keep their name with their address.")
	field(c, "Contact name", &a.contactName, "Jordan Lee")
	field(c, "Contact email", &a.contactEmail, "jordan@company.com")
	action(c, "plus", "Save contact", true, func() {
		address := mail.Address{Name: a.contactName, Address: a.contactEmail}
		if _, err := mail.ParseAddress(address.String()); err != nil || !strings.Contains(a.contactEmail, "@") {
			a.notice("Enter a valid contact email address.", true)
			return
		}
		s := a.store
		value := address.String()
		a.run("Contact saved", func() error { return s.SaveContact(value) })
		a.contactName = ""
		a.contactEmail = ""
	})
	for _, contact := range a.state.Contacts {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, contact).FontSize(12).Grow(1).Selectable()
			textButton(c, "Write email").OnClick(func() {
				a.newComposer(nil, "new")
				if co := a.activeComposer(); co != nil {
					co.toTokens = []string{contact}
					co.dirty = true
				}
			})
			textButton(c, "Remove contact").OnClick(func() {
				s, value := a.store, contact
				a.run("Contact removed", func() error { return s.DeleteContact(value) })
			})
		})
	}
}
