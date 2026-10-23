"""pip install ./examples/vendor/sdk/python ./sdk/python before running."""
import argparse
import getpass
import json
from pathlib import Path
import sys
from licentivo_launch import Application, WindowsDPAPIStore


def main():
    if len(sys.argv) >= 4 and sys.argv[1] == "--config":
        launch = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8-sig"))
        product = json.loads(Path(sys.argv[3]).read_text(encoding="utf-8-sig"))
        if launch.get("schema_version") != 1 or launch.get("licentivo_product_id") != product.get("product_id"):
            raise ValueError("Launch and SDK configuration mismatch")
        sys.argv = [sys.argv[0], launch["merchant_origin"], launch["launch_product_id"], sys.argv[3], "--environment", launch["environment"], *sys.argv[4:]]
    parser = argparse.ArgumentParser()
    parser.add_argument("merchant_origin")
    parser.add_argument("launch_product_id")
    parser.add_argument("product_config", type=Path)
    parser.add_argument("--environment", choices=("sandbox", "live"), default="live")
    parser.add_argument("--remember", action="store_true", help="Windows current-user DPAPI remembered login")
    parser.add_argument("--sign-out", action="store_true")
    parser.add_argument("--release-device", action="store_true")
    args = parser.parse_args()
    if args.sign_out and args.release_device:
        parser.error("Choose either --sign-out or --release-device")
    store = WindowsDPAPIStore() if args.remember else None
    config = json.loads(args.product_config.read_text(encoding="utf-8-sig"))
    with Application(args.merchant_origin, args.launch_product_id, config, args.environment, store=store) as app:
        if args.sign_out:
            app.logout()
            print("Signed out locally")
            return
        if not app.restore():
            app.login(input("Email: ").strip(), getpass.getpass())
        if args.release_device:
            app.release_device()
            print("Device released and signed out")
            return
        app.run_feature("export", lambda: Path("licensed-report.txt").write_text("Protected report\n", encoding="utf-8"))
        print("Protected export completed")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(str(error) + "\nCheck your merchant account center for purchase and authorization status.", file=sys.stderr)
        sys.exit(1)
