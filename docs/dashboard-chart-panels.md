# Bar, pie, and gauge panels

Choose **Bar**, **Pie**, or **Gauge** in the dashboard panel editor. The query
builder supports spans, traces, logs, and metrics. These types also work in
dashboard YAML and the dashboard HTTP/MCP interfaces.

Bar and pie queries return `label` and `value` columns, one row per category:

```sql
SELECT service_name AS label, count(*) AS value
FROM spans
WHERE session_id = $session_id
GROUP BY service_name
ORDER BY value DESC
```

Bars support positive and negative values with a shared zero baseline. Pie
panels show each category's percentage of the total. Pie values must be
non-negative; an all-zero result shows an empty state. Null and nonnumeric
values are omitted from both charts.

Gauge queries return a `value` column. The first row supplies the displayed
value, so aggregate the query to one row or explicitly order and limit it.
For example, `SELECT count(*) AS value FROM spans` shows the span count.
Gauges default to a range of 0–100. Set **Minimum**, **Maximum**, and **Unit**
in the editor, or supply renderer settings such as:

```json
{"min": 0, "max": 1000, "unit": "requests"}
```

The gauge arc stays within its bounds while the label retains the actual
value and reports when it is outside the range. The maximum must exceed the
minimum. All three panels support a `unit` setting, and previews use the same
settings and renderers as saved panels.
