export function CatalogSql({ value }: { value: string }) {
  const tokens = value.split(
    /('(?:''|[^'])*'|\b(?:SELECT|FROM|WHERE|GROUP|BY|ORDER|LIMIT|AS|AND|OR|CASE|WHEN|THEN|ELSE|END|DESC|ASC|COUNT|AVG|SUM|MIN|MAX|CAST|DISTINCT)\b|\b\d+(?:\.\d+)?\b)/gi,
  )
  return (
    <code className="mt-1 block whitespace-pre-wrap break-words font-mono text-[10px] leading-4 text-muted-foreground">
      {tokens.map((token, index) => {
        if (/^'/.test(token))
          return (
            <span key={index} className="text-[var(--sql-string)]">
              {token}
            </span>
          )
        if (/^\d/.test(token))
          return (
            <span key={index} className="text-[var(--sql-number)]">
              {token}
            </span>
          )
        if (
          /^(select|from|where|group|by|order|limit|as|and|or|case|when|then|else|end|desc|asc|count|avg|sum|min|max|cast|distinct)$/i.test(
            token,
          )
        )
          return (
            <span key={index} className="font-semibold text-[var(--sql-keyword)]">
              {token}
            </span>
          )
        return token
      })}
    </code>
  )
}
