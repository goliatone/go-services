package sqlstore

import "github.com/goliatone/go-services/core"

var (
	_ core.ConnectionStore        = (*ConnectionStore)(nil)
	_ core.CredentialStore        = (*CredentialStore)(nil)
	_ core.GrantStore             = (*GrantStore)(nil)
	_ core.StoreProvider          = (*RepositoryFactory)(nil)
	_ core.RepositoryStoreFactory = (*RepositoryFactory)(nil)
)
