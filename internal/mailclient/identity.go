package mailclient

import "errors"

func SendingIdentity(account Account, id string) (Account, error) {
	if id == "" {
		return account, nil
	}
	for _, identity := range account.Identities {
		if identity.ID == id {
			account.Name = identity.Name
			account.Email = identity.Email
			account.ReplyTo = identity.ReplyTo
			account.Signature = identity.Signature
			return account, nil
		}
	}
	return Account{}, errors.New("the selected sending identity no longer exists")
}
