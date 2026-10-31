[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](issue-references.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](issue-references.en.md)

# References to an issue, a merge request and a Jira task

The team wants every commit to explain why it exists: to reference at least one GitLab issue or merge request, or a Jira task. A message may have several references, and they may stand anywhere: in the header, in the body or in the trailers. The header is Conventional Commits.

## Which references are recognized

GitLab itself links a commit to an issue or a merge request when the message references them: this is crosslinking ([GitLab documentation](https://docs.gitlab.com/user/project/issues/crosslinking_issues/)). The policy accepts the same forms of references as GitLab, and keys of Jira tasks:

| Form | Example | What it references |
| --- | --- | --- |
| `#number` | `#121` | an issue of this project |
| `project#number` | `backend#121` | an issue of another project of the same group |
| `group/project#number` | `prozorro-sale/prozorro-deployment#321`, `group/sub/project#1` | an issue of any project, also in a subgroup |
| `!number`, `group/project!number` | `!45`, `prozorro-sale/app!12` | a merge request |
| a full GitLab URL | `https://gitlab.example.com/group/project/-/issues/7`, `.../-/merge_requests/7`, `.../-/work_items/7` | an issue, a merge request or a work item, also on another instance |
| a key of a Jira task | `INFRA-4390`, `https://jira.example.com/browse/INFRA-4390` | a Jira task of the projects listed in the policy |

The hook checks only the text of the message: it asks neither GitLab nor Jira whether the issue, the merge request or the task really exists.

## Policy

Save it as `COMMITPOLICY` in the root of the repository, check it with `gitlab-repopolicy validate -f COMMITPOLICY` and merge it into the default branch.

```yaml
# examples/issue-references/COMMITPOLICY
# Every commit references at least one GitLab issue or merge request, or a
# task of the Jira projects INFRA and PAY, anywhere in the message. GitLab
# references are the forms that GitLab crosslinks: #12, project#12,
# group/project#12, !12 for a merge request and full URLs.
version: 1

rules:
  references:
    match: all
    parser: conventional
    except:
      merge_commit: true
      git_reverts: true
    checks:
      header-format:
        conventional: { level: error }
      pattern:
        issue-or-mr: { level: error, location: message, value: '/(^|[^A-Za-z0-9_])([A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)*)?[#!][0-9]+\b|https?:\/\/\S+\/-\/(issues|merge_requests|work_items)\/[0-9]+|\b(INFRA|PAY)-[0-9]+\b/' }
    message: >-
      Reference a GitLab issue or merge request, or a Jira task, anywhere in
      the message, for example "issue: #121", "close: group/project#321" or
      "jira: INFRA-4390".
```

`location: message` looks for a reference in the whole message, so it does not matter where it stands: `fix: round refunds (#12)` in the header, `See !45` in the body or `close: group/project#321` in a trailer. A regex in `/.../` without `!` must be found at least once, so one reference is enough, and several do not get in the way.

The regex has three parts joined by `|`, and a match of any of them is enough:

| Part of the regex | What it finds |
| --- | --- |
| `(^\|[^A-Za-z0-9_])([A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)*)?[#!][0-9]+\b` | a GitLab reference: `#` for an issue or `!` for a merge request, the number, and before them a project with groups if needed. Before the reference stands the start of a line or a non-letter, so `#fff` and `C#` do not pass: a number must follow `#` |
| `https?:\/\/\S+\/-\/(issues\|merge_requests\|work_items)\/[0-9]+` | a full URL of a GitLab issue, merge request or work item |
| `\b(INFRA\|PAY)-[0-9]+\b` | a key of a Jira task of the projects `INFRA` and `PAY`, also inside a Jira URL |

One check with alternatives is the only way to write "at least one of several". The checks of a rule apply together (AND): two `pattern` checks, one for GitLab and one for Jira, would require both references in every commit.

A GitLab merge commit has the header `Merge branch ...`, and `header-format` would not accept it, so the rule skips it (`except.merge_commit`). It has a reference to the merge request anyway: GitLab writes `See merge request group/project!12`. A `git revert` message has no reference, `except.git_reverts` skips it.

## Your Jira project

The Jira keys are listed in the third part of the regex: `(INFRA|PAY)`. To add the project `OPS`, append it with `|`:

```text
\b(INFRA|PAY|OPS)-[0-9]+\b
```

The list of projects is needed because the general form of a key `[A-Z]+-[0-9]+` matches many ordinary words. Then the commit `fix: use SHA-256 for checksums` would pass as a reference to the task `SHA-256`, and so would `UTF-8` or `ISO-8601`. With the list only the keys of your projects pass.

The form `GL-121`, which the GitLab documentation mentions for messages that cannot start with `#`, is accepted by this policy if `GL` is added to the list of projects: in form it is the same kind of key.

If references must stand only in trailers, for example `issue:`, `jira:` and `close:`, replace `location: message` with `location: trailer` and add `keys: [issue, jira, close]`. Then a reference in the header or the body does not count.

## Messages and checking

Save a message with actual line breaks in `message.txt` and run `gitlab-repopolicy lint-commit -f COMMITPOLICY -m "$(cat message.txt)"`. In the table `\n` stands for a line break.

<!-- check: lint-commit examples/issue-references/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `auth: update broker legal name\n\nissue: #121\n\njira: INFRA-4390\n\nclose: prozorro-sale/prozorro-deployment#321` | passes | three references; one is enough |
| `fix: round refunds (#12)` | passes | a reference to an issue in the header |
| `fix: round refunds\n\nSee !45 for the review` | passes | a merge request in the body |
| `fix: round refunds\n\nRefs: prozorro-sale/sub/project!12` | passes | a merge request of a project in a subgroup |
| `fix: round refunds\n\nhttps://gitlab.example.com/group/project/-/issues/7` | passes | a full URL of an issue |
| `fix: round refunds\n\njira: PAY-12` | passes | a key of a Jira task |
| `fix: round refunds\n\nhttps://jira.example.com/browse/INFRA-4390` | passes | the Jira URL contains the key of the task |
| `fix: round refunds` | denied | there are no references |
| `fix: use SHA-256 for checksums` | denied | `SHA` is not in the list of Jira projects |
| `fix: colour #fff in the header` | denied | no number follows `#` |

The denial of a commit without a reference:

```text
$ gitlab-repopolicy lint-commit -f COMMITPOLICY -m "fix: round refunds"
message: [references] error: pattern issue-or-mr: no match of /(^|[^A-Za-z0-9_])([A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)*)?[#!][0-9]+\b|https?:\/\/\S+\/-\/(issues|merge_requests|work_items)\/[0-9]+|\b(INFRA|PAY)-[0-9]+\b/ in the message
```

The denial prints the whole regex, not an explanation in plain words. So the `message` of the rule says which references are needed, with examples: the author of the commit sees this text at the push.

The rule has `match: all` and by default `check_history: new-content`: every commit is checked when it first appears in the repository, in any branch. There is nothing to check again at a merge into `main` ([comparison of `new-content` and `incoming`](check-history.en.md)).

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
