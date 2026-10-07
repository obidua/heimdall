package types

import (
	"encoding/json"
	"fmt"
	"sort"
)

//
// Gensis state
//

// GenesisState - all auth state that must be provided at genesis
type GenesisState struct {
	Params   Params          `json:"params" yaml:"params"`
	Accounts GenesisAccounts `json:"accounts" yaml:"accounts"`

	// Exported states opt in to restoring signing metadata. Fresh genesis keeps
	// allocating account numbers as before. The counter may contain gaps.
	PreserveAccountNumbers bool    `json:"preserve_account_numbers,omitempty" yaml:"preserve_account_numbers,omitempty"`
	NextAccountNumber      *uint64 `json:"next_account_number,omitempty" yaml:"next_account_number,omitempty"`
}

// NewGenesisState - Create a new genesis state
func NewGenesisState(params Params, accounts GenesisAccounts) GenesisState {
	return GenesisState{
		Params:   params,
		Accounts: SanitizeGenesisAccounts(accounts),
	}
}

// DefaultGenesisState - Return a default genesis state
func DefaultGenesisState() GenesisState {
	return NewGenesisState(DefaultParams(), GenesisAccounts{})
}

// GetGenesisStateFromAppState returns x/auth GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(appState map[string]json.RawMessage) GenesisState {
	var genesisState GenesisState
	if appState[ModuleName] != nil {
		ModuleCdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return genesisState
}

// SetGenesisStateToAppState sets state into app state
func SetGenesisStateToAppState(appState map[string]json.RawMessage, accounts GenesisAccounts) (map[string]json.RawMessage, error) {
	authState := GetGenesisStateFromAppState(appState)
	authState.Accounts = accounts

	appState[ModuleName] = ModuleCdc.MustMarshalJSON(authState)

	return appState, nil
}

// ValidateGenesis performs basic validation of auth genesis data returning an
// error for any failed validation criteria.
func ValidateGenesis(data GenesisState) error {
	if err := data.Params.Validate(); err != nil {
		return err
	}

	if err := ValidateGenAccounts(data.Accounts); err != nil {
		return err
	}
	if !data.PreserveAccountNumbers {
		if data.NextAccountNumber != nil {
			return fmt.Errorf("next account number requires preserve_account_numbers")
		}
		return nil
	}
	if data.NextAccountNumber == nil {
		return fmt.Errorf("preserved auth genesis requires next account number")
	}
	numbers := make(map[uint64]bool, len(data.Accounts))
	for _, account := range data.Accounts {
		if numbers[account.AccountNumber] {
			return fmt.Errorf("duplicate account number in preserved auth genesis: %d", account.AccountNumber)
		}
		if account.AccountNumber >= *data.NextAccountNumber {
			return fmt.Errorf("next account number must exceed all restored account numbers")
		}
		numbers[account.AccountNumber] = true
	}
	return nil
}

// SanitizeGenesisAccounts sorts accounts and coin sets.
func SanitizeGenesisAccounts(genAccs GenesisAccounts) GenesisAccounts {
	sort.Slice(genAccs, func(i, j int) bool {
		return genAccs[i].AccountNumber < genAccs[j].AccountNumber
	})

	for _, acc := range genAccs {
		acc.Coins = acc.Coins.Sort()
	}

	return genAccs
}

// ValidateGenAccounts validates an array of GenesisAccounts and checks for duplicates
func ValidateGenAccounts(accounts GenesisAccounts) error {
	addrMap := make(map[string]bool, len(accounts))

	for _, acc := range accounts {
		// check for duplicated accounts
		addrStr := acc.Address.String()
		if _, ok := addrMap[addrStr]; ok {
			return fmt.Errorf("duplicate account found in genesis state; address: %s", addrStr)
		}

		addrMap[addrStr] = true

		// check account specific validation
		if err := acc.Validate(); err != nil {
			return fmt.Errorf("invalid account found in genesis state; address: %s, error: %s", addrStr, err.Error())
		}
	}

	return nil
}
