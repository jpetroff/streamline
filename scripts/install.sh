#!/bin/sh
set -eu

# Replace this default with the published tarball for the target OS/architecture.
# An environment override is also supported for testing or alternate releases.
TARBALL_URL="${STREAMLINE_TARBALL_URL:-https://example.com/streamline.tar.gz}"

install_streamline() (
    : "${HOME:?HOME must be set}"
    install_dir="$HOME/.local/streamline"
    bin_dir="$HOME/.local/bin"

    for command in curl tar mktemp mkdir cp mv ln rm chmod find; do
        command -v "$command" >/dev/null 2>&1 || {
            printf 'Required command not found: %s\n' "$command" >&2
            exit 1
        }
    done

    if [ "$TARBALL_URL" = 'https://example.com/streamline.tar.gz' ]; then
        printf '%s\n' 'Set TARBALL_URL in the installer or set STREAMLINE_TARBALL_URL to a published build archive.' >&2
        exit 1
    fi
    if [ -d "$bin_dir/streamline" ] || { [ -e "$bin_dir/streamline" ] && [ ! -L "$bin_dir/streamline" ]; }; then
        printf 'Refusing to replace an existing file or directory: %s\n' "$bin_dir/streamline" >&2
        exit 1
    fi
    if [ -d "$install_dir/streamline" ]; then
        printf 'Expected an executable, found a directory: %s\n' "$install_dir/streamline" >&2
        exit 1
    fi

    printf 'Installing Streamline\n  Application directory: %s\n  Launcher directory: %s\n' "$install_dir" "$bin_dir"
    printf 'Creating directories (if needed): %s, %s\n' "$HOME/.local" "$bin_dir"
    mkdir -p "$HOME/.local" "$bin_dir"
    # Stage on the installation filesystem so replacing a running binary works.
    staging_dir=$(mktemp -d "$HOME/.local/.streamline-install.XXXXXX")
    trap 'printf "Removing temporary download and extracted files: %s\n" "$staging_dir"; rm -rf "$staging_dir"' 0
    trap 'exit 1' HUP INT TERM
    mkdir "$staging_dir/payload"
    printf 'Downloading build archive:\n  From: %s\n  To:   %s\n' "$TARBALL_URL" "$staging_dir/build.tar.gz"
    curl -fSL --silent --show-error "$TARBALL_URL" -o "$staging_dir/build.tar.gz"
    printf 'Extracting archive:\n  From: %s\n  To:   %s\nFiles extracted:\n' "$staging_dir/build.tar.gz" "$staging_dir/payload"
    tar -xzvf "$staging_dir/build.tar.gz" -C "$staging_dir/payload"
    if [ ! -f "$staging_dir/payload/streamline" ] || [ -L "$staging_dir/payload/streamline" ]; then
        printf '%s\n' 'The archive must contain a streamline executable at its root.' >&2
        exit 1
    fi

    printf 'Setting executable permissions (755): %s\n' "$staging_dir/payload/streamline"
    chmod 755 "$staging_dir/payload/streamline"
    printf 'Staging executable:\n  From: %s\n  To:   %s\n' "$staging_dir/payload/streamline" "$staging_dir/streamline"
    mv "$staging_dir/payload/streamline" "$staging_dir/streamline"
    printf 'Creating application directory (if needed): %s\n' "$install_dir"
    mkdir -p "$install_dir"
    printf 'Copying supplementary files:\n  From: %s\n  To:   %s\nFiles to copy (relative to these directories):\n' "$staging_dir/payload" "$install_dir"
    (cd "$staging_dir/payload" && find . ! -name . -print)
    cp -R "$staging_dir/payload/." "$install_dir/"
    printf 'Installing executable (replacing any previous version):\n  From: %s\n  To:   %s\n' "$staging_dir/streamline" "$install_dir/streamline"
    mv -f "$staging_dir/streamline" "$install_dir/streamline"
    # Release assets are embedded; keep the caller's working directory intact.
    printf 'Linking launcher: %s -> %s\n' "$bin_dir/streamline" "$install_dir/streamline"
    ln -s "$install_dir/streamline" "$staging_dir/launcher"
    mv -f "$staging_dir/launcher" "$bin_dir/streamline"

    printf 'Installed Streamline to %s\n' "$install_dir"
    case ":${PATH:-}:" in
        *":$bin_dir:"*) ;;
        *) printf '%s\n' 'Add this to your shell profile: export PATH="$HOME/.local/bin:$PATH"' ;;
    esac
)

# Keep all work inside a function so this script also works when piped to sh.
install_streamline
