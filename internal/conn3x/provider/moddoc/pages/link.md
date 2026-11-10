Use `link` to manage a symbolic link on a host and the path it points to.

## Example usage

Point a link at a file:

```hcl
step "python" {
  link {
    path   = "/usr/local/bin/python"
    target = "/usr/bin/python3"
  }
}
```

Use a relative target, resolved from the link's own directory:

```hcl
step "current_release" {
  link {
    path   = "/srv/app/current"
    target = "releases/2026-10-01"
  }
}
```

Make sure a link is gone, and stays gone:

```hcl
step "old_python" {
  link {
    path  = "/usr/local/bin/python2"
    state = "absent"
  }
}
```
<!-- reference -->
## How it works

**Reading.** Every plan reads the link on the host with `readlink` and compares its target with `target`, as written. Conn3x doesn't resolve the target or check that it exists. If a regular file or directory is at the path, Conn3x stops with an error and doesn't replace it.

**Absent.** With `state = "absent"`, every plan checks only whether a link exists at the path. If one does, the plan shows the step out of sync and apply removes it. If not, the plan shows no change, and a link that comes back later is removed again by the next apply. Conn3x removes the link itself, never the file or directory it points to, and removes a link whose target is missing too. `target` can't be set alongside it. A regular file, a directory or anything else that isn't a link at the path is an error, and is left in place.

**Applying.** Conn3x creates the new link in a temporary directory beside the path and renames it over the old one, so the path always points somewhere while the link changes. If the rename isn't possible, it falls back to `ln -sfn`.

## Removal

When you delete a `link` step, `on_remove` decides what happens on the host. The default, `"forget"`, leaves the link where it is. `"delete"` removes the link, never the file or directory it points to. If something other than a link is at the path by then, Conn3x stops with an error and leaves it alone.

## From Ansible

`conn3x convert ansible` turns `file` tasks with `state: link` into `link` steps, with `src` becoming `target`.
