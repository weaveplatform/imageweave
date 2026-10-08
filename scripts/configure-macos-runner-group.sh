#!/usr/bin/env bash
# Administrator bootstrap only; never give organization credentials to image jobs.
set -euo pipefail
org=weaveplatform
repository="$org/imageweave"
workflow="$repository/.github/workflows/macos-candidate.yml@refs/heads/main"
group_id=$(gh api "orgs/$org/actions/runner-groups" --paginate \
  --jq '.runner_groups[] | select(.name == "imageweave-macos" and .inherited == false) | .id')
[[ "$group_id" =~ ^[0-9]+$ ]] || { echo 'Expected one dedicated imageweave-macos runner group' >&2; exit 1; }
# Close access first, even if the dedicated group was accidentally broadened.
gh api --method PATCH "orgs/$org/actions/runner-groups/$group_id" --input - \
  <<< '{"visibility":"selected","restricted_to_workflows":true,"selected_workflows":[]}' > /dev/null
gh api --method PUT "orgs/$org/actions/runner-groups/$group_id/repositories" \
  --input - <<< '{"selected_repository_ids":[]}'
gh api "repos/$repository/contents/.github/workflows/macos-candidate.yml?ref=main" --silent
repository_id=$(gh api "repos/$repository" --jq .id)
[[ "$repository_id" =~ ^[0-9]+$ ]] || exit 1
jq -n --arg workflow "$workflow" \
  '{visibility:"selected",allows_public_repositories:true,restricted_to_workflows:true,selected_workflows:[$workflow]}' |
  gh api --method PATCH "orgs/$org/actions/runner-groups/$group_id" --input - > /dev/null
# Read back the restriction before enabling this public repository.
gh api "orgs/$org/actions/runner-groups/$group_id" |
  jq -e --arg workflow "$workflow" '.visibility == "selected" and .allows_public_repositories == true and .restricted_to_workflows == true and .selected_workflows == [$workflow]' > /dev/null
jq -n --argjson id "$repository_id" '{selected_repository_ids:[$id]}' |
  gh api --method PUT "orgs/$org/actions/runner-groups/$group_id/repositories" --input -
gh api "orgs/$org/actions/runner-groups/$group_id/repositories" |
  jq -e --argjson id "$repository_id" '.total_count == 1 and [.repositories[].id] == [$id]' > /dev/null
echo 'macOS runner pool restricted to imageweave/macos-candidate.yml on main'
