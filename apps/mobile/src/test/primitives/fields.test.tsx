import { fireEvent, render, screen } from '@testing-library/react-native'
import { Platform, View } from 'react-native'
import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { Checkbox } from '../../components/ui/Checkbox'
import { FormField } from '../../components/ui/FormField'
import { Label } from '../../components/ui/Label'
import { Switch } from '../../components/ui/Switch'
import { TextArea } from '../../components/ui/TextArea'
import { TextField } from '../../components/ui/TextField'
import { FIELDS_REGISTRY } from '../../components/ui/fields.registry'
import { setColorScheme } from '../color-scheme'
import { COLOR_SCHEMES, resolvedStyle } from './helpers'

const TOUCH_MIN = nativeTokens.light.touchTarget.min

describe('FormField', () => {
  it('sets accessibilityState.invalid and announces the error in a polite live region', () => {
    render(
      <FormField label="Email" error="Email is required">
        <TextField testID="email" />
      </FormField>,
    )

    expect(screen.getByTestId('email').props.accessibilityState.invalid).toBe(true)
    const error = screen.getByText('Email is required')
    expect(error.props.accessibilityLiveRegion).toBe('polite')
  })

  it('does not mark the control invalid when there is no error', () => {
    render(
      <FormField label="Email">
        <TextField testID="email" />
      </FormField>,
    )

    expect(screen.getByTestId('email').props.accessibilityState.invalid).toBe(false)
    expect(screen.queryByText('Email is required')).toBeNull()
  })

  it('associates the label with the control accessible name and forwards the hint', () => {
    render(
      <FormField label="Email" hint="We never share it" required>
        <TextField testID="email" />
      </FormField>,
    )

    const control = screen.getByTestId('email')
    expect(control.props.accessibilityLabel).toBe('Email required')
    expect(control.props.accessibilityHint).toBe('We never share it')
  })

  it('keeps the error in the accessibility tree, not only in style', () => {
    render(
      <FormField label="Email" error="Invalid address">
        <TextField testID="email" />
      </FormField>,
    )

    expect(screen.getByText('Invalid address')).toBeTruthy()
  })

  it('propagates disabled to the control', () => {
    render(
      <FormField label="Email" disabled>
        <TextField testID="email" />
      </FormField>,
    )

    const control = screen.getByTestId('email')
    expect(control.props.editable).toBe(false)
    expect(control.props.accessibilityState.disabled).toBe(true)
  })
})

describe('Label', () => {
  it('renders its text and marks required', () => {
    render(
      <Label testID="label" required>
        Email
      </Label>,
    )

    expect(screen.getByText(/Email/)).toBeTruthy()
    expect(screen.getByText(/\*/)).toBeTruthy()
  })

  it('renders a disabled label without crashing', () => {
    render(
      <Label testID="label" disabled>
        Email
      </Label>,
    )

    expect(screen.getByText('Email')).toBeTruthy()
  })
})

describe('TextField', () => {
  it('forwards the documented props', () => {
    const onChangeText = jest.fn()
    render(
      <TextField
        testID="text"
        value="hello"
        onChangeText={onChangeText}
        placeholder="Email"
        keyboardType="email-address"
        autoComplete="email"
        maxLength={10}
        editable
      />,
    )

    const node = screen.getByTestId('text')
    expect(node.props.value).toBe('hello')
    expect(node.props.placeholder).toBe('Email')
    expect(node.props.keyboardType).toBe('email-address')
    expect(node.props.autoComplete).toBe('email')
    expect(node.props.maxLength).toBe(10)
    expect(node.props.editable).toBe(true)

    fireEvent.changeText(node, 'world')
    expect(onChangeText).toHaveBeenCalledWith('world')
  })

  it('meets the touch-target floor', () => {
    render(<TextField testID="text" />)
    expect(resolvedStyle('text').minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
  })

  it('does not suppress dynamic type', () => {
    render(<TextField testID="text" />)
    expect(screen.getByTestId('text').props.allowFontScaling).not.toBe(false)
  })

  it('still exposes its label when secureTextEntry is set', () => {
    render(
      <FormField label="Password">
        <TextField testID="password" secureTextEntry />
      </FormField>,
    )

    const node = screen.getByTestId('password')
    expect(node.props.secureTextEntry).toBe(true)
    expect(node.props.accessibilityLabel).toBe('Password')
  })
})

describe('TextArea', () => {
  it('is multiline and honours numberOfLines', () => {
    render(<TextArea testID="area" numberOfLines={4} />)

    const node = screen.getByTestId('area')
    expect(node.props.multiline).toBe(true)
    expect(node.props.numberOfLines).toBe(4)
  })

  it('meets the touch-target floor', () => {
    render(<TextArea testID="area" />)
    expect(resolvedStyle('area').minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
  })
})

describe('Switch', () => {
  it('exposes the switch role and checked state', () => {
    render(<Switch testID="switch" value onValueChange={() => {}} />)

    const node = screen.getByTestId('switch')
    expect(node.props.accessibilityRole).toBe('switch')
    expect(node.props.accessibilityState.checked).toBe(true)
  })

  it('toggles through onValueChange', () => {
    const onValueChange = jest.fn()
    render(<Switch testID="switch" value={false} onValueChange={onValueChange} />)

    fireEvent.press(screen.getByTestId('switch'))
    expect(onValueChange).toHaveBeenCalledWith(true)
  })

  it('meets the touch-target floor', () => {
    render(<Switch testID="switch" value={false} />)
    expect(resolvedStyle('switch').minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
  })
})

describe('Checkbox', () => {
  it('exposes the checkbox role and checked state', () => {
    render(<Checkbox testID="checkbox" checked />)

    const node = screen.getByTestId('checkbox')
    expect(node.props.accessibilityRole).toBe('checkbox')
    expect(node.props.accessibilityState.checked).toBe(true)
  })

  it('maps indeterminate to the mixed checked state', () => {
    render(<Checkbox testID="checkbox" checked={false} indeterminate />)

    expect(screen.getByTestId('checkbox').props.accessibilityState.checked).toBe('mixed')
  })

  it('toggles through onCheckedChange', () => {
    const onCheckedChange = jest.fn()
    render(<Checkbox testID="checkbox" checked={false} onCheckedChange={onCheckedChange} />)

    fireEvent.press(screen.getByTestId('checkbox'))
    expect(onCheckedChange).toHaveBeenCalledWith(true)
  })

  it('meets the touch-target floor', () => {
    render(<Checkbox testID="checkbox" checked={false} />)
    expect(resolvedStyle('checkbox').minHeight as number).toBeGreaterThanOrEqual(TOUCH_MIN)
  })
})

describe('malformed input', () => {
  it('renders empty and very long label/error strings without crashing', () => {
    const long = 'x'.repeat(5000)
    render(
      <FormField label="" error={long}>
        <TextField testID="text" />
      </FormField>,
    )

    expect(screen.getByText(long)).toBeTruthy()
  })
})

describe('field primitives in both themes', () => {
  for (const scheme of COLOR_SCHEMES) {
    it(`renders every control in ${scheme}`, () => {
      setColorScheme(scheme)
      render(
        <View>
          <FormField label="Email" hint="hint" error="error">
            <TextField testID={`text-${scheme}`} />
          </FormField>
          <TextArea testID={`area-${scheme}`} />
          <Switch testID={`switch-${scheme}`} value />
          <Checkbox testID={`checkbox-${scheme}`} checked />
        </View>,
      )

      expect(screen.getByTestId(`text-${scheme}`)).toBeTruthy()
      expect(screen.getByTestId(`area-${scheme}`)).toBeTruthy()
      expect(screen.getByTestId(`switch-${scheme}`)).toBeTruthy()
      expect(screen.getByTestId(`checkbox-${scheme}`)).toBeTruthy()
    })
  }
})

describe('web ARIA state', () => {
  it('puts aria-invalid on the TextField on web', () => {
    const platform = jest.replaceProperty(Platform, 'OS', 'web')
    try {
      render(
        <View>
          <TextField testID="web-invalid" invalid />
          <TextField testID="web-valid" />
        </View>,
      )

      expect(screen.getByTestId('web-invalid').props['aria-invalid']).toBe(true)
      expect(screen.getByTestId('web-valid').props['aria-invalid']).toBe(false)
    } finally {
      platform.restore()
    }
  })

  it('does not put aria-invalid on the TextField on native', () => {
    render(<TextField testID="native-invalid" invalid />)

    expect(screen.getByTestId('native-invalid').props['aria-invalid']).toBeUndefined()
  })
})

describe('FIELDS_REGISTRY', () => {
  it('carries valid entries whose tokens resolve in both themes', () => {
    expect(FIELDS_REGISTRY.length).toBeGreaterThan(0)
    for (const entry of FIELDS_REGISTRY) {
      expect(entry.name.length).toBeGreaterThan(0)
      expect(entry.component).toBeDefined()
      expect(typeof entry.interactive).toBe('boolean')
      for (const scheme of COLOR_SCHEMES) {
        const section = { ...nativeTokens[scheme].color, ...nativeTokens[scheme].contrast } as Record<string, string>
        expect({ entry: entry.name, scheme, token: entry.foregroundToken }).toEqual({
          entry: entry.name,
          scheme,
          token: expect.any(String),
        })
        expect(section[entry.foregroundToken]).toBeDefined()
        expect(section[entry.backgroundToken]).toBeDefined()
      }
    }
  })

  it('pins the FormField.error pair to the danger contrast token', () => {
    expect(FIELDS_REGISTRY.find((entry) => entry.name === 'FormField.error')).toMatchObject({
      foregroundToken: 'bb-danger-text',
      backgroundToken: 'background',
      ratioClass: 'body',
    })
  })
})