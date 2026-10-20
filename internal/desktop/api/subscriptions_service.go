package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type SubscriptionsService struct{ app *app.App }

func (s *SubscriptionsService) ManageSubscriptionAccounts(input app.SubscriptionAccountCommand) (app.SubscriptionAccountResult, error) {
	return s.app.ManageSubscriptionAccounts(input)
}

func (s *SubscriptionsService) ManageSubscriptionConnection(input app.SubscriptionConnectionCommand) (app.SubscriptionConnectionResult, error) {
	return s.app.ManageSubscriptionConnection(input)
}
