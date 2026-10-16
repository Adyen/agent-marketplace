# Terminal basics

`agent-marketplace` runs in a terminal. If you rarely use one, this page covers what the rest of the docs need. If you use a shell every day, skip to [Install](./install.md).

New words are in the [glossary](./glossary.md).

## Open a terminal

A terminal is an app where you type commands instead of clicking buttons.

- On macOS, press `Cmd` + `Space`, type `Terminal`, and press `Enter`. You can also find it in Finder under **Applications** > **Utilities** > **Terminal**.
- On Linux, press `Ctrl` + `Alt` + `T` on most desktops, or search for "Terminal" in the applications menu.
- Code editors such as Cursor and Windsurf have a built-in terminal, usually under a **Terminal** menu. It works the same way.

`agent-marketplace` is released for macOS and Linux only. There is no Windows build.

## Run a command

The terminal shows a prompt, often ending in `$` or `%`, followed by a blinking cursor. Type a command and press `Enter` to run it. The command prints its output below, and the prompt comes back when it has finished.

Try this. It prints the `agent-marketplace` version once the program is installed:

```bash
agent-marketplace --version
```

Commands appear in grey boxes. To run one:

1. Copy the text from the box. Most boxes have a copy button in the top-right corner.
2. Paste it at the prompt: `Cmd` + `V` on macOS, or `Ctrl` + `Shift` + `V` in most Linux terminals.
3. Press `Enter`.

A few things in those boxes are worth recognizing:

- A line that starts with `#` is a comment. The terminal ignores it, so you can paste it or leave it out.
- A line that ends with `\` continues on the next line. Copy all the lines together.
- A block that starts with `cat > some-file <<'EOF'` and ends with a line containing only `EOF` creates a file with the text in between. Copy the whole block, including the final `EOF` line.
- Text in angle brackets, such as `<url>` or `<name>`, is a placeholder. Replace it, brackets included, with your own value before you press `Enter`.

To stop a command that is still running, press `Ctrl` + `C`.

## Know which folder you are in

A terminal works in one folder, the current directory. Commands such as `agent-marketplace sync` act on the project in that folder, so move into your project first.

| Command | What it does |
|---|---|
| `pwd` | Prints the folder you are in. |
| `ls` | Lists the files in that folder. |
| `cd ~/code/my-project` | Moves into the folder `code/my-project` inside your home folder. |
| `cd ..` | Moves up one folder. |
| `cd` | Moves back to your home folder. |

`~` is short for your home folder: `/Users/<your-name>` on macOS and `/home/<your-name>` on Linux. So `~/.agents/config.yaml` means the file `config.yaml` in the folder `.agents` inside your home folder.

On macOS, you can type `cd ` (with a space) and then drag a folder from Finder onto the Terminal window. The terminal fills in the folder's full path.

## See hidden files and folders

Files and folders whose names start with a dot are hidden. `agent-marketplace` uses several of them, for example `.agents/` for configuration and `.claude/` or `.cursor/` for the files it installs for your coding tool.

- In the terminal, `ls -a` lists hidden entries too.
- In the macOS Finder, press `Cmd` + `Shift` + `.` to show or hide them.

## Edit a configuration file

Configuration files are plain text in a format called [YAML](./glossary.md#yaml). Open them in a code editor such as Cursor, VS Code, or Windsurf. Word processors and rich-text editors can replace straight quotes with curly ones, which breaks the file.

To edit without leaving the terminal, use `nano`:

```bash
nano .agents/config.yaml
```

Type your changes, press `Ctrl` + `O` and then `Enter` to save, and press `Ctrl` + `X` to exit.

When you edit YAML:

- Indent with spaces. Tabs are not allowed.
- Keep the indentation of the examples. It decides which setting belongs to which.
- Lists can be written one item per line with `- ` in front, or on one line as `[claude, cursor]`.

## Make a command available everywhere

When you type `agent-marketplace`, the terminal looks for a program with that name in a list of folders called the `PATH`. You can print the list with:

```bash
echo $PATH
```

If the terminal answers `command not found: agent-marketplace` (or `agent-marketplace: command not found`), the program is not in any of those folders yet. [Install](./install.md) shows where to put it.

To see which copy the terminal finds, run:

```bash
which agent-marketplace
```

## Install Git

Most registries are Git repositories, and `agent-marketplace` uses the `git` program to download them. Check that Git is installed:

```bash
git --version
```

If it prints a version such as `git version 2.x`, you are ready. If it is missing:

- On macOS, running `git --version` offers to install the Command Line Tools, which include Git. Accept and wait for the installation to finish.
- On Debian or Ubuntu, run `sudo apt install git`. On Fedora, run `sudo dnf install git`.

## The interactive screen

Running `agent-marketplace` with nothing after it opens a full-screen interface inside the terminal. You control it with the keyboard:

- `j` and `k`, or the up and down arrow keys, move through a list.
- `space` opens the choices for the highlighted row. In a checklist, it ticks or unticks the highlighted box.
- `enter` opens the details of the highlighted row, and `esc` goes back.
- `?` shows every key you can press.
- `q` quits. The TUI saves your changes to the configuration as you make them, but if some of them haven't been synced to your harness folders yet, it asks whether to sync before quitting. In the file browser, `q` goes back to the list instead.
- `Ctrl` + `C` quits immediately.

The [TUI reference](./reference/tui.md) lists every key.

## When something goes wrong

- Read the last few lines of the output. Errors and warnings usually include a code such as `v4.undefined-source`. Type that code into the search box at the top of this site to find the page that explains it. The [diagnostics reference](./reference/diagnostics.md) explains how the codes are named.
- `command not found` means the program is not on your `PATH`. See [Make a command available everywhere](#make-a-command-available-everywhere).
- `permission denied` usually means one of two things. The file is not marked as a program yet (run `chmod +x` on it), or you are writing to a system folder, which needs `sudo` in front of the command. `sudo` asks for your computer password. Nothing appears on screen while you type it, which is normal.
- Some commands report a problem through their exit status without printing much. Run `echo $?` right after a command: `0` means it succeeded, anything else means it did not. For example, `agent-marketplace sync --check` exits with `1` when the files on disk are out of date.
- Add `--debug` to any `agent-marketplace` command to print detailed logs to the terminal while it runs; while the TUI is on screen they go only to the log file. By default, logs also go to `~/.agent-marketplace/logs/agent-marketplace.log`. The [logging reference](./reference/logging.md) covers log levels and rotation.

## Next

- [Install](./install.md) `agent-marketplace`.
- Follow the [quick start](./quick-start.md).
- Keep the [glossary](./glossary.md) open for unfamiliar terms.
