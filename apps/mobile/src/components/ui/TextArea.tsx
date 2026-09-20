import { TextField, type TextFieldProps } from './TextField'

export interface TextAreaProps extends TextFieldProps {
  /** Visible line count. Defaults to 4. */
  numberOfLines?: number
}

/**
 * A multiline text input. It reuses `TextField`'s token styling, touch-target floor and
 * accessibility wiring, and pins the text to the top of the box.
 */
export function TextArea({ numberOfLines = 4, style, ...rest }: TextAreaProps) {
  return (
    <TextField
      {...rest}
      multiline
      numberOfLines={numberOfLines}
      style={[{ textAlignVertical: 'top' }, style]}
    />
  )
}