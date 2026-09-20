# Accessibility device checks — pending, not skipped

The jest suite in this directory asserts every **enforceable** #1656 criterion against the
`PRIMITIVE_REGISTRY` (roles/names, focus order, keyboard close paths + focus ring, dynamic type,
reduced motion, contrast, touch targets, error announcements, style constraints under representative
dimensions). It is deliberately silent about four things it **cannot** observe, because
jest/react-test-renderer has no native accessibility tree, no layout engine and no web DOM. They are
listed here so they are reported visibly rather than passing by omission.

## 1. Native VoiceOver / TalkBack traversal

**Why it cannot be asserted here.** jest runs the React component tree, not a device accessibility
tree. There is no `UIAccessibility` / `AccessibilityNodeInfo` to walk, so "the screen reader reaches
every control in a sensible order and reads the right label" is a device behaviour.

**How to run it.**
- iOS (VoiceOver): `bun run --cwd apps/mobile ios` on a physical device or simulator, then
  Settings → Accessibility → VoiceOver, and swipe through every primitive on the gallery screen.
- Android (TalkBack): `bun run --cwd apps/mobile android` on a physical device or emulator, then
  Settings → Accessibility → TalkBack.
- Device: the smallest supported phone (≤ 320pt wide) and a tablet, because the traversal order can
  differ with layout.

**Status:** PENDING — no device run recorded in this change.

## 2. Dynamic type at 200% on a real device

**Why it cannot be asserted here.** The suite asserts that no primitive sets
`allowFontScaling={false}` and that `Text` forwards `maxFontSizeMultiplier`, but jest has no font
metrics, so it cannot prove that text at 200% is not clipped or overlapped. Only a real renderer
lays out the scaled glyphs.

**How to run it.**
- iOS: Settings → Accessibility → Display & Text Size → Larger Text → maximum, then relaunch and
  inspect every primitive.
- Android: Settings → Accessibility → Font size → Largest, and Display size → Largest.
- Device: the smallest supported phone (320×568) is the binding case.

**Status:** PENDING — no device run recorded in this change.

## 3. Native modal focus (focus trap and focus restore)

**Why it cannot be asserted here.** RN `Modal` on native does not implement web focus management,
and jest has no DOM. The browser half is asserted in Todo 22(e) via Playwright
(`apps/mobile/e2e/visual/gallery.spec.ts`), which opens and closes every overlay and asserts focus
lands inside and returns to the trigger.

**How to run it.**
- Web (authoritative): `bun run --cwd apps/mobile e2e:visual` (Todo 22).
- Native spot check: iOS/Android with a hardware keyboard or Switch Control, open a
  `Dialog`/`ConfirmDialog`/`BottomSheet`/`ActionSheet` and confirm focus cannot leave the overlay.

**Status:** PENDING on native; the browser half is owned by Todo 22(e).

## 4. Content clipping and parent overflow

**Why it cannot be asserted here.** jest has no layout engine, so "content is clipped" and "a layout
primitive overflows its parent" are unobservable. This suite asserts style constraints only (no
fixed width/height exceeding the viewport; text has no hard pixel width). Todo 22(f) asserts the two
geometric properties in a real browser at 320×568, 375×812, 768×1024, 1024×768 and 1440×900.

**How to run it.**
- `bun run --cwd apps/mobile e2e:visual` (Todo 22(f)).

**Status:** PENDING — owned by Todo 22(f).

## Representative dimensions used by this suite

| Profile | Size | Why it is representative |
|---|---|---|
| small phone | 320×568 | The narrowest width still in the support matrix (iPhone SE 1st gen / older Android). It is the binding constraint for every fixed dimension, so a primitive that fits here fits the larger phones. |
| standard phone | 390×844 | The current mainstream iPhone size and the common Android logical width. The default development target. |
| tablet | 834×1112 | A 4:3-ish tablet in portrait. The largest logical width the primitives must not over-stretch to; it also exercises the wide-layout branch of `BottomSheet`/`TabBar`. |

The three profiles are chosen so that the smallest is the **worst case for overflow** and the largest
is the **worst case for over-stretch**. A primitive that satisfies the style constraints at both
ends satisfies them in between.
