const { getDefaultConfig } = require('expo/metro-config')
const { withNativewind } = require('nativewind/metro')
const exclude = require('@boostedbudget/design-tokens/nativewind-exclude.json')

module.exports = withNativewind(getDefaultConfig(__dirname), {
  inlineVariables: { exclude },
})