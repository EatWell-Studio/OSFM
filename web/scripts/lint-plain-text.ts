// ESLint rules for src/: text is always rendered as plain text (docs/architecture.md §4.7). React escapes what it
// renders, so the ways to put a string into the page as HTML are the escape hatches below; they are all refused. A
// member's text that should keep line breaks uses CSS (white-space: pre-line), and links are built as elements.

import type { Linter } from 'eslint'

const message = 'User text is rendered as plain text; build elements instead of HTML strings (§4.7).'

export const plainText: Linter.Config = {
  name: 'osfm/plain-text',
  files: ['src/**/*.{ts,tsx}'],
  rules: {
    'no-restricted-syntax': [
      'error',
      // <div dangerouslySetInnerHTML={...}>, <iframe srcDoc={...}>
      { selector: 'JSXAttribute[name.name=/^(dangerouslySetInnerHTML|srcDoc)$/]', message },
      // createElement('div', { dangerouslySetInnerHTML: ... })
      { selector: 'Property[key.name="dangerouslySetInnerHTML"]', message },
      // element.innerHTML = ..., element.outerHTML += ..., iframe.srcdoc = ...
      {
        selector: 'AssignmentExpression > MemberExpression.left[property.name=/^(innerHTML|outerHTML|srcdoc)$/]',
        message,
      },
      // element.insertAdjacentHTML(...), range.createContextualFragment(...), element.setHTMLUnsafe(...)
      {
        selector:
          'CallExpression > MemberExpression.callee[property.name=/^(insertAdjacentHTML|createContextualFragment|setHTMLUnsafe)$/]',
        message,
      },
    ],
    'no-restricted-properties': [
      'error',
      { object: 'document', property: 'write', message },
      { object: 'document', property: 'writeln', message },
    ],
  },
}
