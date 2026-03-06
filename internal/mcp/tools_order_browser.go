package mcp

import (
	"context"
	"errors"

	"one-agent/internal/platforms"
	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/types"
)

type browserOrderPlatform interface {
	PlaceOrderViaBrowser(ctx context.Context, productID, addressID, paymentToken string, quantity int) (types.Order, error)
}

type browserSessionMetadataHydrator interface {
	HydrateBrowserSessionMetadata(ctx context.Context) (*types.AppSession, error)
}

func (s *Server) sessionForAppWithMetadata(
	ctx context.Context,
	app types.Platform,
	wantAddresses, wantPayments bool,
) (*types.AppSession, *toolError) {
	session, err := s.sessionForApp(ctx, app)
	if err != nil {
		return nil, err
	}
	if !needsBrowserMetadata(session, wantAddresses, wantPayments) {
		return session, nil
	}
	client, clientErr := s.platform(app)
	if clientErr != nil {
		return nil, clientErr
	}
	hydrated, hydrateErr := hydrateBrowserMetadata(ctx, client)
	if hydrateErr != nil {
		return nil, hydrateErr
	}
	if hydrated != nil {
		return hydrated, nil
	}
	return session, nil
}

func needsBrowserMetadata(session *types.AppSession, wantAddresses, wantPayments bool) bool {
	if session == nil {
		return false
	}
	if wantAddresses && len(session.Addresses) == 0 {
		return true
	}
	return wantPayments && len(session.Payments) == 0
}

func hydrateBrowserMetadata(ctx context.Context, client platforms.Platform) (*types.AppSession, *toolError) {
	hydrator, ok := client.(browserSessionMetadataHydrator)
	if !ok {
		return nil, nil
	}
	session, err := hydrator.HydrateBrowserSessionMetadata(ctx)
	if err == nil {
		return session, nil
	}
	if errors.Is(err, blinkit.ErrBrowserWorkerNotConfigured) {
		return nil, nil
	}
	return nil, internalError("browser session metadata refresh failed", err)
}
