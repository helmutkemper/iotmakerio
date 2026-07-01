// markdown.js — lightweight Markdown → HTML renderer (no dependencies)

export function md(src) {
    if (!src) return '';

    let h = src
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;');

    // Fenced code blocks
    h = h.replace(/```[\w]*\n([\s\S]*?)```/g, (_, c) =>
        `<pre><code>${c.trim()}</code></pre>`);

    // Inline code
    h = h.replace(/`([^`\n]+)`/g, '<code>$1</code>');

    // Headings
    h = h
        .replace(/^#### (.+)$/gm, '<h4>$1</h4>')
        .replace(/^### (.+)$/gm,  '<h3>$1</h3>')
        .replace(/^## (.+)$/gm,   '<h2>$1</h2>')
        .replace(/^# (.+)$/gm,    '<h1>$1</h1>');

    // Horizontal rule
    h = h.replace(/^---+$/gm, '<hr>');

    // Blockquote
    h = h.replace(/^&gt; (.+)$/gm, '<blockquote><p>$1</p></blockquote>');

    // Bold + italic
    h = h
        .replace(/\*\*\*(.+?)\*\*\*/g, '<strong><em>$1</em></strong>')
        .replace(/\*\*(.+?)\*\*/g,     '<strong>$1</strong>')
        .replace(/\*(.+?)\*/g,         '<em>$1</em>');

    // Images before links
    h = h.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img alt="$1" src="$2">');

    // Links
    h = h.replace(/\[([^\]]+)\]\(([^)]+)\)/g,
        '<a href="$2" target="_blank" rel="noopener">$1</a>');

    // Unordered lists
    h = h.replace(/(^[*\-] .+\n?)+/gm, m =>
        `<ul>${m.trim().split('\n')
            .map(l => `<li>${l.replace(/^[*\-] /, '')}</li>`)
            .join('')}</ul>`);

    // Ordered lists
    h = h.replace(/(^\d+\. .+\n?)+/gm, m =>
        `<ol>${m.trim().split('\n')
            .map(l => `<li>${l.replace(/^\d+\. /, '')}</li>`)
            .join('')}</ol>`);

    // Paragraphs (wrap double-newline-separated blocks)
    h = h.split(/\n{2,}/).map(b => {
        b = b.trim();
        if (!b || /^<(h[1-6]|ul|ol|pre|blockquote|hr)/.test(b)) return b;
        return `<p>${b.replace(/\n/g, '<br>')}</p>`;
    }).join('\n');

    return h;
}
