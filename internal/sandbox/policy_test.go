package sandbox_test

import (
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestPolicyConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, config.PolicyAllowAll, config.NetworkPolicy("allow-all"))
	assert.Equal(t, config.PolicyBalanced, config.NetworkPolicy("balanced"))
	assert.Equal(t, config.PolicyDenyAll, config.NetworkPolicy("deny-all"))
}
