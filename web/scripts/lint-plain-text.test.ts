// @vitest-environment node
import { Linter } from 'eslint'
import tseslint from 'typescript-eslint'
import { describe, expect, it } from 'vitest'
import { plainText } from './lint-plain-text.ts'

function lint(code: string): string[] {
  const linter = new Linter({ configType: 'flat' })
  const messages = linter.verify(
    code,
    [
      {
        files: ['**/*.tsx'],
        languageOptions: { parser: tseslint.parser, parserOptions: { ecmaFeatures: { jsx: true } } },
        rules: plainText.rules,
      },
    ],
    'src/Example.tsx',
  )
  return messages.map((m) => `${String(m.line)}: ${m.ruleId ?? m.message}`)
}

describe('plain-text lint rules', () => {
  it('refuse every way of rendering a string as HTML', () => {
    const code = [
      'export const A = (p: { html: string }) => <div dangerouslySetInnerHTML={{ __html: p.html }} />',
      "export const B = (p: { html: string }) => createElement('div', { dangerouslySetInnerHTML: { __html: p.html } })",
      'export function c(el: HTMLElement, s: string) { el.innerHTML = s }',
      'export function d(el: HTMLElement, s: string) { el.outerHTML += s }',
      "export function e(el: HTMLElement, s: string) { el.insertAdjacentHTML('beforeend', s) }",
      'export function f(s: string) { document.write(s) }',
      'export function g(r: Range, s: string) { return r.createContextualFragment(s) }',
      'export const H = (p: { html: string }) => <iframe srcDoc={p.html} />',
      'export function i(el: HTMLIFrameElement, s: string) { el.srcdoc = s }',
    ].join('\n')
    expect(lint(code)).toEqual([
      '1: no-restricted-syntax',
      '2: no-restricted-syntax',
      '3: no-restricted-syntax',
      '4: no-restricted-syntax',
      '5: no-restricted-syntax',
      '6: no-restricted-properties',
      '7: no-restricted-syntax',
      '8: no-restricted-syntax',
      '9: no-restricted-syntax',
    ])
  })

  it('allow plain text', () => {
    const code = [
      'export const A = (p: { text: string }) => <p className="whitespace-pre-line">{p.text}</p>',
      'export function b(el: HTMLElement, s: string) { el.textContent = s; return el.innerHTML.length }',
    ].join('\n')
    expect(lint(code)).toEqual([])
  })
})
