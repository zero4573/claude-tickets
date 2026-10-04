# Builds the fixtures of the ct sync tests: jq -n --arg state 1|2|3 -f fixtures.jq
def st($name; $cat): {name: $name, statusCategory: {key: $cat}};
def todo: st("To Do"; "new");
def doing: st("In Progress"; "indeterminate");
def review: st("In Review"; "indeterminate");
def done: st("Done"; "done");
def typ($n): {name: $n, hierarchyLevel: (if $n == "Epic" then 1 else 0 end)};
def jane: {accountId: "u-me", displayName: "Jane Doe"};
def bob: {accountId: "u-bob", displayName: "Bob Roe"};
def blockedby($k; $s): {type: {name: "Blocks", inward: "is blocked by", outward: "blocks"}, inwardIssue: {key: $k, fields: {status: $s}}};
def relates($k): {type: {name: "Relates", inward: "relates to", outward: "relates to"}, outwardIssue: {key: $k}};
def md($d): {appliedContentFormat: "markdown", fields: {description: $d}};
def comment($id; $at; $who; $body): {id: $id, created: $at, updated: $at, author: $who, body: $body};
def cl($at; $fields): {total: 1, histories: [{created: $at, items: ($fields | map({field: .}))}]};
def issue($t; $sum; $s; $who): {issuetype: typ($t), summary: $sum, status: $s, assignee: $who,
  reporter: bob, priority: {name: "Medium"}, fixVersions: [], components: [], labels: [], issuelinks: [], subtasks: []};

($state | tonumber) as $n
| "2026-09-01T10:00:00.000-0400" as $u1
| "2026-09-10T10:00:00.000-0400" as $u2
| "2026-09-20T10:00:00.000-0400" as $u3
| (if $n >= 2 then done else todo end) as $blocker
| {
  me: "u-me",
  resources: [{cloudId: "cloud-1", url: "https://acme.atlassian.net/", name: "acme", products: [{id: "jira"}]},
              {cloudId: "cloud-2", url: "https://acme-wiki.atlassian.net", products: [{id: "confluence"}]}],
  pageSize: 4,
  sse: ($n == 2),
  open: (["PROJ-1", "PROJ-2", "PROJ-3", "PROJ-4", "PROJ-5", "PROJ-7", "PROJ-8", "PROJ-10", "PROJ-11", "PROJ-12", "PROJ-16"]
    | if $n >= 2 then map(select(. != "PROJ-2" and . != "PROJ-3" and . != "PROJ-8")) + ["PROJ-6"] else . end
    | if $n == 3 then . + ["PROJ-2"] else . end),
  issues: ({
    "PROJ-1": {
      fields: (issue("Story"; "Add billing export"; doing; jane) + {
        priority: {name: "High"}, fixVersions: [{name: "1.2"}, {name: "1.3"}], components: [{name: "Billing"}],
        labels: ["export", "csv"], updated: (if $n >= 2 then $u2 else $u1 end),
        issuelinks: [blockedby("OTHER-9"; $blocker), relates("PROJ-3")],
        subtasks: [{key: "PROJ-7", fields: {summary: "Write the export job", status: todo}}]}),
      changelog: cl($u2; ["timeoriginalestimate", "rank"]),
      evidence: {appliedContentFormat: "markdown", fields: {description: "Export invoices as CSV.\n\n- daily\n- weekly  \n\n",
        customFields: {"Acceptance Criteria": {value: "CSV opens in a spreadsheet  "}, "Steps to Reproduce": {value: "   "}}}},
      comments: {appliedContentFormat: "markdown", comments: [
        comment("101"; "2026-09-02T09:00:00.000-0400"; bob; "Looks good.\nShip it\n\nthanks | cheers"),
        comment(99; "2026-09-01T09:00:00.000-0400"; jane; "First pass done")]}},
    "PROJ-2": {
      fields: (issue("Bug"; "Totals wrong: rounding"; (if $n == 2 then done elif $n == 3 then doing else todo end); jane)
        + {priority: {name: "Low"}, updated: (if $n == 3 then $u3 elif $n == 2 then $u2 else $u1 end)}),
      changelog: cl($u3; ["status"]),
      evidence: {appliedContentFormat: "html", fields: {description: "<p>panel</p>"}}},
    "PROJ-3": {
      fields: (issue("Task"; "Clean up logs"; review; (if $n >= 2 then bob else jane end)) + {updated: (if $n >= 2 then $u2 else $u1 end)}),
      changelog: cl($u2; ["assignee"]),
      evidence: md("Remove noisy logs."),
      comments: {appliedContentFormat: "html", comments: [comment("301"; $u1; bob; "<p>see @Jane</p>")]}},
    "PROJ-4": {
      fields: (issue("Epic"; "Billing v2"; doing; jane) + {updated: $u1}),
      evidence: md("The second billing.")},
    "PROJ-5": {
      fields: (issue("Story"; "Invoice PDF"; todo; jane) + {updated: (if $n >= 2 then $u2 else $u1 end),
        parent: {key: "PROJ-4", fields: {summary: "Billing v2", issuetype: typ("Epic")}}}),
      changelog: cl($u2; ["description", "Sprint"]),
      evidence: md(if $n >= 2 then "Render invoices as PDF, A4." else "Render invoices as PDF." end),
      comments: {appliedContentFormat: "markdown", comments: ([comment("501"; $u1; bob; "Which paper size?")]
        + if $n >= 2 then [comment("502"; $u2; jane; "A4")] else [] end)}},
    "PROJ-6": {
      fields: (issue("Story"; "Refund flow"; doing; (if $n >= 2 then jane else bob end)) + {updated: (if $n >= 2 then $u2 else $u1 end),
        parent: {key: "PROJ-4", fields: {summary: "Billing v2", issuetype: typ("Epic")}}}),
      evidence: md("Refunds.")},
    "PROJ-7": {
      fields: (issue("Task"; "Write the export job"; todo; jane) + {updated: $u1,
        parent: {key: "PROJ-20", fields: {summary: "Platform", issuetype: typ("Epic")}}}),
      evidence: md("Cron job.")},
    "PROJ-8": {
      fields: (issue("Spike"; "Spike: try streaming"; todo; jane) + {updated: $u1, priority: null}),
      evidence: {appliedContentFormat: "markdown", fields: {}}},
    "PROJ-10": {
      fields: (issue("Task"; "Old task"; todo; jane) + {updated: $u1, issuelinks: [blockedby("OTHER-9"; todo)]}),
      evidence: md("Waits for OTHER-9.")},
    "PROJ-11": {
      fields: (issue("Task"; "Rate limits"; (if $n >= 2 then review else doing end); jane) + {updated: (if $n >= 2 then $u2 else $u1 end)}),
      changelog: {total: 30, histories: [{created: $u2, items: [{field: "status"}]}]},
      evidence: md(if $n >= 2 then "Limit to 10 rps." else "Limit requests." end)},
    "PROJ-12": {
      fields: (issue("Task"; "Legacy note"; doing; jane) + {updated: $u1}),
      evidence: md("From the old layout.")},
    "PROJ-16": {
      fields: (issue("Task"; "Broken"; todo; jane) + {updated: $u1}),
      failGet: ($n == 1)},
    "PROJ-20": {
      fields: (issue("Epic"; "Platform"; doing; bob) + {updated: (if $n >= 2 then $u2 else $u1 end)}),
      changelog: cl($u2; ["labels"]),
      evidence: md("Platform work.")},
    "OTHER-9": {
      fields: (issue("Task"; "Upstream fix"; $blocker; bob) + {updated: $u1})}
  } | if $n >= 2 then del(.["PROJ-8"]) else . end)
}
