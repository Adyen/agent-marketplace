package config

import "errors"

func (r Registry) Validate(policy Policy) error {
	return errors.Join(validateRegistry(0, r, policy)...)
}
