#!/usr/bin/env python3
"""
Telegram session manager — one-time bootstrap + ongoing session checks.

Unlike the Twitter org (which pulls live cookies from the host browser via
browser_cookie3), Telegram uses MTProto — a custom protocol with no browser
cookie surface. So we do **one interactive bootstrap** (phone + SMS code,
~30 seconds) and Telethon persists a `.session` SQLite file that's reused
forever. After that one-time auth, every subsequent op is fully automated.

Usage:
  python3 session.py --setup-credentials API_ID API_HASH PHONE
                                                # Write credentials file (do this first)
  python3 session.py --bootstrap                # Interactive login: sends SMS, you enter code
  python3 session.py --check                    # Verify session is alive (calls get_me)
  python3 session.py --info                     # Show session info without using it
  python3 session.py --logout                   # Revoke session

Getting API credentials:
  1. Go to https://my.telegram.org/apps
  2. Sign in with your phone number
  3. Click "API development tools"
  4. Fill the form (any app name + short name works)
  5. Copy api_id (int) and api_hash (string)
  6. Run: python3 session.py --setup-credentials 12345 abcdef phone_here

Files written:
  /sandbox/.telegram-credentials.json   # api_id, api_hash, phone
  /sandbox/.telegram-session.session    # Telethon SQLite session (auth state)
"""
import argparse
import asyncio
import json
import os
import sys
from datetime import datetime

CREDENTIALS_PATH = "/sandbox/.telegram-credentials.json"
SESSION_PATH = "/sandbox/.telegram-session.session"


def credentials_exist():
    return os.path.exists(CREDENTIALS_PATH) and os.path.getsize(CREDENTIALS_PATH) > 0


def session_exist():
    return os.path.exists(SESSION_PATH) and os.path.getsize(SESSION_PATH) > 0


def load_credentials():
    """Return dict with api_id (int), api_hash (str), phone (str). None if missing."""
    if not credentials_exist():
        return None
    with open(CREDENTIALS_PATH) as f:
        data = json.load(f)
    # api_id stored as JSON number — ensure int
    if isinstance(data.get("api_id"), str):
        try:
            data["api_id"] = int(data["api_id"])
        except ValueError:
            pass
    return data


def save_credentials(api_id, api_hash, phone):
    """Write credentials file. api_id can be int or numeric string."""
    try:
        api_id_int = int(api_id)
    except (ValueError, TypeError):
        print(json.dumps({
            "error": f"api_id must be an integer, got: {api_id!r}",
        }))
        sys.exit(2)

    if not api_hash or not isinstance(api_hash, str):
        print(json.dumps({"error": "api_hash must be a non-empty string"}))
        sys.exit(2)

    if not phone:
        print(json.dumps({"error": "phone is required (format: +15551234567)"}))
        sys.exit(2)

    data = {
        "api_id": api_id_int,
        "api_hash": api_hash,
        "phone": phone,
        "saved_at": datetime.now().isoformat(),
    }
    with open(CREDENTIALS_PATH, "w") as f:
        json.dump(data, f, indent=2)
    os.chmod(CREDENTIALS_PATH, 0o600)
    print(json.dumps({
        "ok": True,
        "path": CREDENTIALS_PATH,
        "api_id": api_id_int,
        "phone": phone,
        "next_step": "python3 /sandbox/telegram_session.py --bootstrap",
    }, indent=2))


def _get_client():
    """Build a TelegramClient from saved credentials. Does not connect."""
    try:
        from telethon import TelegramClient
    except ImportError:
        print(json.dumps({
            "error": "telethon not installed. Add 'telethon' to pux.yaml pip_packages.",
        }))
        sys.exit(3)

    creds = load_credentials()
    if not creds:
        print(json.dumps({
            "error": f"No credentials at {CREDENTIALS_PATH}.",
            "hint": "Run: python3 /sandbox/telegram_session.py --setup-credentials API_ID API_HASH PHONE",
        }))
        sys.exit(4)

    return TelegramClient(
        SESSION_PATH,
        creds["api_id"],
        creds["api_hash"],
    ), creds


def bootstrap_interactive():
    """One-time interactive login.

    Sends an SMS code to the saved phone number, prompts the user for the
    code, signs in, and saves the session file. If 2FA is enabled, also
    prompts for the password.

    Run this once. After success, the .session file persists and you never
    need to run it again unless you explicitly --logout or the session dies.
    """
    client, creds = _get_client()

    if session_exist():
        # Check if already authorized — don't double-bootstrap
        client.connect()
        try:
            if client.is_user_authorized():
                me = client.get_me()
                print(json.dumps({
                    "ok": True,
                    "already_authorized": True,
                    "user": _safe_user(me),
                    "session_path": SESSION_PATH,
                    "hint": "Session is live. Use --check to verify, or just start using telegram_helpers.",
                }, indent=2))
                return
        finally:
            client.disconnect()

    # Not authorized — start the bootstrap flow
    print(f"Sending SMS code to {creds['phone']}...", file=sys.stderr)
    client.connect()
    try:
        client.send_code_request(creds["phone"])
        code = input(">>> Enter the code you received: ").strip()
        try:
            client.sign_in(creds["phone"], code)
        except Exception as e:
            # 2FA password required?
            if "Two-steps verification" in str(e) or "SESSION_PASSWORD_NEEDED" in str(e):
                password = input(">>> Enter your 2FA password: ").strip()
                client.sign_in(password=password)
            else:
                raise

        me = client.get_me()
        print(json.dumps({
            "ok": True,
            "authorized": True,
            "user": _safe_user(me),
            "session_path": SESSION_PATH,
            "saved_at": datetime.now().isoformat(),
        }, indent=2))
    finally:
        client.disconnect()


def check_session():
    """Verify the saved session is still alive by calling get_me.

    Returns JSON status. Doesn't modify anything. Reports the FIRST missing
    prerequisite so the user gets a clean linear setup path.
    """
    if not credentials_exist():
        return {
            "valid": False,
            "reason": f"No credentials at {CREDENTIALS_PATH}",
            "next_step": "python3 /sandbox/telegram_session.py --setup-credentials API_ID API_HASH PHONE",
            "hint": "Get api_id + api_hash from https://my.telegram.org/apps",
        }
    if not session_exist():
        return {
            "valid": False,
            "reason": f"No session file at {SESSION_PATH}",
            "next_step": "python3 /sandbox/telegram_session.py --bootstrap",
        }

    client, _ = _get_client()
    client.connect()
    try:
        if not client.is_user_authorized():
            return {
                "valid": False,
                "reason": "Session file exists but Telegram no longer recognizes it. "
                          "Session may have been revoked.",
                "next_step": "python3 /sandbox/telegram_session.py --bootstrap",
            }
        me = client.get_me()
        return {
            "valid": True,
            "user": _safe_user(me),
            "session_path": SESSION_PATH,
            "checked_at": datetime.now().isoformat(),
        }
    except Exception as e:
        return {
            "valid": False,
            "reason": f"Telethon error: {e}",
        }
    finally:
        client.disconnect()


def show_info():
    """Print session + credentials info without making API calls."""
    out = {
        "credentials_path": CREDENTIALS_PATH,
        "session_path": SESSION_PATH,
    }
    if credentials_exist():
        creds = load_credentials()
        out["credentials"] = {
            "api_id": creds.get("api_id"),
            "phone": creds.get("phone"),
            "saved_at": creds.get("saved_at"),
            # intentionally NOT including api_hash
        }
    else:
        out["credentials"] = None
    out["session_exists"] = session_exist()
    if session_exist():
        out["session_size_bytes"] = os.path.getsize(SESSION_PATH)
    print(json.dumps(out, indent=2))


def logout():
    """Revoke the session. Next use will require --bootstrap again."""
    if not session_exist():
        print(json.dumps({"ok": True, "already_logged_out": True}))
        return
    client, _ = _get_client()
    client.connect()
    try:
        client.log_out()
        print(json.dumps({"ok": True, "logged_out": True}))
    except Exception as e:
        # Fall back to just deleting the file
        try:
            os.remove(SESSION_PATH)
            print(json.dumps({"ok": True, "logged_out": True, "fallback": "file_deleted", "error": str(e)}))
        except Exception as e2:
            print(json.dumps({"ok": False, "error": str(e2)}))
    finally:
        client.disconnect()


def _safe_user(me):
    """Extract non-sensitive fields from a Telethon User object."""
    if me is None:
        return None
    return {
        "id": getattr(me, "id", None),
        "first_name": getattr(me, "first_name", None),
        "last_name": getattr(me, "last_name", None),
        "username": getattr(me, "username", None),
        "phone": getattr(me, "phone", None),
        "is_bot": getattr(me, "bot", False),
    }


def main():
    parser = argparse.ArgumentParser(description="Telegram session manager")
    parser.add_argument("--setup-credentials", nargs=3, metavar=("API_ID", "API_HASH", "PHONE"),
                        help="Write api_id, api_hash, phone to credentials file")
    parser.add_argument("--bootstrap", action="store_true",
                        help="Interactive login: sends SMS code, you enter it (one-time)")
    parser.add_argument("--check", action="store_true",
                        help="Verify saved session is alive (calls get_me)")
    parser.add_argument("--info", action="store_true",
                        help="Show session + credentials info without API calls")
    parser.add_argument("--logout", action="store_true",
                        help="Revoke session (next use requires --bootstrap)")
    args = parser.parse_args()

    if args.setup_credentials:
        api_id, api_hash, phone = args.setup_credentials
        save_credentials(api_id, api_hash, phone)
    elif args.bootstrap:
        bootstrap_interactive()
    elif args.check:
        print(json.dumps(check_session(), indent=2))
    elif args.info:
        show_info()
    elif args.logout:
        logout()
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
