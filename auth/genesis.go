package auth

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	authTypes "github.com/maticnetwork/heimdall/auth/types"
)

// InitGenesis - Init store state from genesis data
func InitGenesis(ctx sdk.Context, ak AccountKeeper, processors []authTypes.AccountProcessor, data authTypes.GenesisState) {
	if data.PreserveAccountNumbers {
		if err := authTypes.ValidateGenesis(data); err != nil {
			panic(err)
		}
		// Construction may already have created a module account. A complete
		// exported state must include it before resetting the allocation counter.
		ak.IterateAccounts(ctx, func(existing authTypes.Account) bool {
			if !data.Accounts.Contains(existing.GetAddress()) {
				panic("preserved auth genesis omits a preinitialized account")
			}
			return false
		})
	}
	ak.SetParams(ctx, data.Params)
	data.Accounts = authTypes.SanitizeGenesisAccounts(data.Accounts)

	for _, gacc := range data.Accounts {
		acc := gacc.ToAccount()

		// convert to base account
		d := acc.(*authTypes.BaseAccount)

		// execute account processors
		for _, p := range processors {
			acc = p(&gacc, d) //nolint
		}

		if !data.PreserveAccountNumbers {
			acc = ak.NewAccount(ctx, acc)
		}
		ak.SetAccount(ctx, acc)
	}
	if data.PreserveAccountNumbers {
		ak.setNextAccountNumber(ctx, *data.NextAccountNumber)
	}
}

// ExportGenesis returns a GenesisState for a given context and keeper
func ExportGenesis(ctx sdk.Context, ak AccountKeeper) authTypes.GenesisState {
	params := ak.GetParams(ctx)

	var genAccounts authTypes.GenesisAccounts

	ak.IterateAccounts(ctx, func(acc authTypes.Account) bool {
		account, err := authTypes.NewGenesisAccountI(acc)
		if err != nil {
			panic(err)
		}
		genAccounts = append(genAccounts, account)
		return false
	})

	result := authTypes.NewGenesisState(params, genAccounts)
	next := ak.PeekNextAccountNumber(ctx)
	result.PreserveAccountNumbers = true
	result.NextAccountNumber = &next
	return result
}
