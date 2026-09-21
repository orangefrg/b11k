"""Offline release regression tests: python3 -B -m unittest discover -s scripts/tests -v."""

import base64
from contextlib import ExitStack, redirect_stdout
import importlib.machinery
import importlib.util
import io
import json
from pathlib import Path
import plistlib
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
loader = importlib.machinery.SourceFileLoader("release_apple", str(Path(__file__).resolve().parents[1] / "release-apple"))
spec = importlib.util.spec_from_loader(loader.name, loader)
release = importlib.util.module_from_spec(spec)
loader.exec_module(release)


def arguments(output, *extra):
    return release.parse_args(["--version", "1.0", "--build", "3", "--output-dir", str(output), *extra])


class ReleaseTestCase(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.output = self.root / "release with spaces"
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        self.stack.enter_context(redirect_stdout(io.StringIO()))


class ReleaseTests(ReleaseTestCase):
    def test_invalid_inputs_fail_before_work(self):
        cases = [("--version", "1.0; touch bad"), ("--version", "01.13"), ("--build", "0"),
                 ("--build", "$(id)"), ("--build", "076"), ("--unsigned", "--distribute", "testflight"),
                 ("--group", "Family"), ("--retry-upload",), ("--processing-timeout", "0")]
        for extra in cases:
            with self.subTest(extra=extra), self.assertRaises(release.ReleaseError):
                arguments(self.output, *extra)
        self.assertFalse(self.output.exists())

    def test_dry_run_has_no_files_processes_or_network(self):
        args = arguments(self.output, "--distribute", "testflight", "--dry-run")
        with patch.object(release.subprocess, "run", side_effect=AssertionError("process started")), \
             patch.object(release, "AppStoreConnect", side_effect=AssertionError("API created")):
            release.release(args)
        self.assertFalse(self.output.exists())

    def test_versions_override_every_target_and_export_cannot_renumber(self):
        args = arguments(self.output)
        for platform in args.platforms:
            for command in (release.archive_command(args, platform, ["", "", ""]), release.test_command(args, platform)):
                self.assertIn("MARKETING_VERSION=1.0", command)
                self.assertIn("CURRENT_PROJECT_VERSION=3", command)
        for destination in ("export", "upload"):
            self.assertIs(release.export_options(args, destination)["manageAppVersionAndBuildNumber"], False)
            self.assertIs(release.export_options(args, destination)["testFlightInternalTestingOnly"], True)

    def test_command_display_redacts_auth_and_quotes_paths(self):
        command = ["xcodebuild", "-archivePath", "/tmp/path with spaces", *release.auth_arguments(["secret.p8", "key-id", "issuer-id"])]
        displayed = release.display_command(command)
        self.assertIn("'/tmp/path with spaces'", displayed)
        for private in ("secret.p8", "key-id", "issuer-id"):
            self.assertNotIn(private, displayed)
        self.assertEqual(displayed.count("<credential>"), 3)

    def test_partial_credentials_fail(self):
        with patch.dict(release.os.environ, {"ASC_KEY_PATH": "missing.p8", "ASC_KEY_ID": "", "ASC_ISSUER_ID": ""}):
            with self.assertRaisesRegex(release.ReleaseError, "together"):
                release.credentials()

    def archive_fixture(self, platform, controls=False, extension_build="3"):
        archive = self.output / (platform + ".xcarchive")
        app = archive / "Products/Applications/B11k.app"
        app.mkdir(parents=True)
        (archive / "Info.plist").write_bytes(plistlib.dumps({"ApplicationProperties": {"ApplicationPath": "Applications/B11k.app"}}))
        info = {"CFBundleIdentifier": release.BUNDLE_ID, "CFBundleShortVersionString": "1.0", "CFBundleVersion": "3"}
        info_path = app / "Info.plist"
        info_path.parent.mkdir(exist_ok=True)
        info_path.write_bytes(plistlib.dumps(info))
        if controls and platform == "ios":
            extension = app / "PlugIns/B11kControls.appex"
            extension.mkdir(parents=True)
            info.update(CFBundleIdentifier=release.BUNDLE_ID + ".controls", CFBundleVersion=extension_build)
            (extension / "Info.plist").write_bytes(plistlib.dumps(info))
        return archive

    def test_archive_bundle_and_versions_validate(self):
        args = arguments(self.output, "--unsigned")
        for platform in args.platforms:
            self.archive_fixture(platform)
            release.verify_archive(args, platform)

    def test_wrong_archive_identity_or_version_blocks_release(self):
        archive = self.archive_fixture("ios")
        info_path = archive / "Products/Applications/B11k.app/Info.plist"
        original = plistlib.loads(info_path.read_bytes())
        for key, value in (("CFBundleIdentifier", "another.app"), ("CFBundleVersion", "2"),
                           ("CFBundleShortVersionString", "2.0")):
            with self.subTest(key=key):
                info_path.write_bytes(plistlib.dumps({**original, key: value}))
                with self.assertRaises(release.ReleaseError):
                    release.verify_archive(arguments(self.output, "--unsigned"), "ios")

    def test_unconfigured_extension_blocks_release(self):
        self.archive_fixture("ios", controls=True)
        with self.assertRaisesRegex(release.ReleaseError, "Unexpected extension"):
            release.verify_archive(arguments(self.output, "--unsigned"), "ios")


    def test_archive_cannot_reference_application_outside_products(self):
        archive = self.archive_fixture("ios")
        (archive / "Info.plist").write_bytes(plistlib.dumps({"ApplicationProperties": {"ApplicationPath": "../../elsewhere.app"}}))
        with self.assertRaisesRegex(release.ReleaseError, "contain one application"):
            release.verify_archive(arguments(self.output, "--unsigned"), "ios")

    def test_signed_archives_are_checked_with_codesign(self):
        self.archive_fixture("ios")
        with patch.object(release, "run") as run:
            release.verify_archive(arguments(self.output), "ios")
        self.assertEqual(run.call_count, 1)
        self.assertEqual(run.call_args.args[0][:4], ["codesign", "--verify", "--strict", "--deep"])

    def test_jwt_signature_can_be_verified_by_openssl(self):
        key = self.root / "test.p8"
        public = self.root / "public.pem"
        subprocess.run(["openssl", "genpkey", "-algorithm", "EC", "-pkeyopt", "ec_paramgen_curve:P-256", "-out", str(key)], check=True, capture_output=True)
        subprocess.run(["openssl", "pkey", "-in", str(key), "-pubout", "-out", str(public)], check=True, capture_output=True)
        with patch.object(release.time, "time", return_value=1000):
            token = release.AppStoreConnect([str(key), "test-key", "test-issuer"]).token()
        header, payload, encoded = token.split(".")
        decode = lambda value: base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))
        self.assertEqual(json.loads(decode(header)), {"alg": "ES256", "kid": "test-key", "typ": "JWT"})
        self.assertEqual(json.loads(decode(payload)), {"iss": "test-issuer", "iat": 1000, "exp": 1600, "aud": "appstoreconnect-v1"})
        raw = decode(encoded)
        self.assertEqual(len(raw), 64)
        integers = []
        for value in (raw[:32], raw[32:]):
            value = value.lstrip(b"\0") or b"\0"
            if value[0] >= 128:
                value = b"\0" + value
            integers.append(b"\x02" + bytes([len(value)]) + value)
        body = b"".join(integers)
        signature = self.root / "signature.der"
        signature.write_bytes(b"\x30" + bytes([len(body)]) + body)
        result = subprocess.run(["openssl", "dgst", "-sha256", "-verify", str(public), "-signature", str(signature)],
                                input=(header + "." + payload).encode(), capture_output=True)
        self.assertEqual(result.returncode, 0)

    def test_api_lookup_distinguishes_platform_and_marketing_version(self):
        api = release.AppStoreConnect(["", "", ""])
        api.collection = Mock(return_value=[])
        api.find_build("app", arguments(self.output), "ios")
        self.assertEqual(api.collection.call_args.args[1], {"filter[app]": "app", "filter[version]": "3",
            "filter[preReleaseVersion.version]": "1.0", "filter[preReleaseVersion.platform]": "IOS"})

    def test_api_rejects_foreign_pagination_before_creating_token(self):
        api = release.AppStoreConnect(["", "", ""])
        api.token = Mock(side_effect=AssertionError("token created"))
        for path in ("https://example.org/v1/builds", "https://api.appstoreconnect.apple.com.evil/v1/builds"):
            with self.assertRaises(release.ReleaseError):
                api.request("GET", path)
        api.token.assert_not_called()

    def test_processing_failure_and_timeout_stop_distribution(self):
        api = release.AppStoreConnect(["", "", ""])
        api.find_build = Mock(return_value={"attributes": {"processingState": "INVALID"}})
        with self.assertRaisesRegex(release.ReleaseError, "failed Apple processing"):
            api.wait_for_build("app", arguments(self.output), "ios")
        api.find_build.return_value = None
        with patch.object(release.time, "monotonic", side_effect=[0, 1201]), self.assertRaisesRegex(release.ReleaseError, "Use --resume"):
            api.wait_for_build("app", arguments(self.output), "ios")

    def test_testflight_assigns_only_requested_groups_and_is_idempotent(self):
        api = Mock()
        api.request.return_value = {"data": {"attributes": {"internalBuildState": "READY_FOR_BETA_TESTING"}}}
        api.collection.side_effect = [[{"id": "build"}], []]
        groups = [{"id": value, "attributes": {"name": value, "isInternalGroup": True}} for value in ("existing", "new")]
        release.distribute_testflight(api, "build", groups)
        mutations = [call.args for call in api.request.call_args_list if call.args[0] != "GET"]
        self.assertEqual(mutations, [("POST", "/v1/betaGroups/new/relationships/builds", {"data": [{"type": "builds", "id": "build"}]})])

    def test_testflight_compliance_issue_fails_even_without_explicit_groups(self):
        api = Mock()
        api.request.return_value = {"data": {"attributes": {"internalBuildState": "MISSING_EXPORT_COMPLIANCE"}}}
        with self.assertRaisesRegex(release.ReleaseError, "MISSING_EXPORT_COMPLIANCE"):
            release.distribute_testflight(api, "build", [])
        api.collection.assert_not_called()


class GroupPreflightTests(ReleaseTestCase):
    def preflight(self, groups, *options):
        api = Mock()
        api.collection.side_effect = [[{"id": "b11k-app"}], groups]
        result = release.preflight_distribution(api, arguments(self.output, "--distribute", "testflight", *options))
        api.request.assert_not_called()
        return result

    def group(self, internal=True, automatic=False):
        return {"id": "group-id", "attributes": {"name": "Internal Testers", "isInternalGroup": internal,
                                                  "hasAccessToAllBuilds": automatic}}

    def test_explicit_internal_group_is_resolved_once(self):
        group = self.group()
        self.assertEqual(self.preflight([group], "--group", "Internal Testers", "--group", "Internal Testers"),
                         ("b11k-app", [group]))

    def test_missing_ambiguous_and_external_groups_fail_before_building(self):
        for groups in ([], [self.group(), self.group()], [self.group(internal=False)]):
            with self.subTest(groups=groups), self.assertRaises(release.ReleaseError):
                self.preflight(groups, "--group", "Internal Testers")

    def test_automatic_distribution_requires_an_internal_automatic_group(self):
        for groups in ([], [self.group()], [self.group(internal=False, automatic=True)]):
            with self.subTest(groups=groups), self.assertRaisesRegex(release.ReleaseError, "automatic distribution"):
                self.preflight(groups)
        self.assertEqual(self.preflight([self.group(automatic=True)]), ("b11k-app", []))

    def test_external_assignment_is_rejected_without_api_mutation(self):
        api = Mock()
        with self.assertRaisesRegex(release.ReleaseError, "Only internal"):
            release.distribute_testflight(api, "build-id", [self.group(internal=False)])
        api.request.assert_not_called()
        api.collection.assert_not_called()


class SigningTests(ReleaseTestCase):
    def test_signed_releases_enable_automatic_provisioning_by_default(self):
        args = arguments(self.output)
        for platform in args.platforms:
            commands = [release.archive_command(args, platform, ["", "", ""])]
            commands += [release.export_command(args, platform, destination, ["", "", ""]) for destination in ("export", "upload")]
            for command in commands:
                self.assertIn("-allowProvisioningUpdates", command)

    def test_provisioning_can_be_disabled_and_explicit_allow_still_works(self):
        for flag, enabled in (("--no-provisioning-updates", False), ("--allow-provisioning-updates", True)):
            args = arguments(self.output, flag)
            for platform in args.platforms:
                commands = [release.archive_command(args, platform, ["", "", ""])]
                commands += [release.export_command(args, platform, destination, ["", "", ""]) for destination in ("export", "upload")]
                for command in commands:
                    self.assertEqual("-allowProvisioningUpdates" in command, enabled)

    def test_unsigned_archives_never_provision_or_send_signing_credentials(self):
        args = arguments(self.output, "--unsigned")
        for platform in args.platforms:
            command = release.archive_command(args, platform, ["private.p8", "key", "issuer"])
            self.assertNotIn("-allowProvisioningUpdates", command)
            self.assertNotIn("-authenticationKeyPath", command)

    def test_xcode_account_signing_does_not_override_account_with_api_key(self):
        creds = ["private.p8", "key", "issuer"]
        for mode in ("xcode", "api-key"):
            args = arguments(self.output, "--signing-auth", mode)
            for platform in args.platforms:
                commands = [release.archive_command(args, platform, creds)]
                commands += [release.export_command(args, platform, destination, creds) for destination in ("export", "upload")]
                for command in commands:
                    self.assertEqual("-authenticationKeyPath" in command, mode == "api-key")
                    self.assertEqual("-authenticationKeyID" in command, mode == "api-key")
                    self.assertEqual("-authenticationKeyIssuerID" in command, mode == "api-key")
        self.assertEqual(creds, ["private.p8", "key", "issuer"])

    def test_signing_failure_hints_distinguish_provisioning_and_cloud_permissions(self):
        disabled = release.signing_failure_hint('No signing certificate "iOS Distribution" found', ["xcodebuild", "-exportArchive"])
        self.assertIn("--allow-provisioning-updates", disabled)
        enabled = release.signing_failure_hint('No signing certificate "iOS Distribution" found', ["xcodebuild", "-exportArchive", "-allowProvisioningUpdates"])
        self.assertIn("Automatic provisioning was enabled", enabled)
        denied = release.signing_failure_hint("Cloud signing permission error; private-secret", ["xcodebuild", "-exportArchive"])
        self.assertIn("--signing-auth xcode", denied)
        self.assertNotIn("private-secret", denied)

    def test_archive_account_error_takes_priority_over_missing_profile_and_ignores_old_attempts(self):
        log = self.root / 'archive.log'
        args = arguments(self.output, '--signing-auth', 'xcode')
        command = release.archive_command(args, 'ios', ['', '', ''])
        output = ['No Account for Team "MUFH94C6T3". private-secret\n'
                  "No profiles for 'com.apetrikov.b11k' were found\n"]

        def failed(command, **kwargs):
            kwargs['stdout'].write(output[0])
            return Mock(returncode=65)

        with patch.object(release.subprocess, 'run', side_effect=failed):
            with self.assertRaises(release.ReleaseError) as error:
                release.run(command, log)
            message = str(error.exception)
            self.assertIn('no authenticated account for signing team MUFH94C6T3', message)
            self.assertIn('--team-id', message)
            self.assertIn('--env-file', message)
            self.assertIn('new --output-dir', message)
            self.assertNotIn('distribution signing', message)
            self.assertNotIn('private-secret', message)
            output[0] = '/tmp/App.swift:10:2: error: compilation failed\n'
            with self.assertRaises(release.ReleaseError) as error:
                release.run(command, log)
            self.assertNotIn('signing team', str(error.exception))

    def test_archive_profile_hint_uses_archive_signing_requirements(self):
        args = arguments(self.output)
        hint = release.signing_failure_hint("No profiles for 'com.apetrikov.b11k' were found",
                                            release.archive_command(args, 'ios', ['', '', '']))
        self.assertIn('archive', hint)
        self.assertIn('selected team owns the bundle ID', hint)
        self.assertNotIn('App Store export requires distribution signing', hint)

    def test_export_error_uses_only_current_attempt_and_does_not_echo_credentials(self):
        log = self.root / "export.log"
        log.write_text('Old failure: No signing certificate "iOS Distribution" found\n')
        output = ["New failure: network unavailable; private-secret\n"]

        def failed(command, **kwargs):
            kwargs["stdout"].write(output[0])
            return Mock(returncode=1)

        with patch.object(release.subprocess, "run", side_effect=failed):
            with self.assertRaises(release.ReleaseError) as error:
                release.run(["xcodebuild", "-exportArchive"], log)
            self.assertNotIn("provisioning", str(error.exception))
            self.assertNotIn("private-secret", str(error.exception))
            output[0] = 'No signing certificate "iOS Distribution" found; private-secret\n'
            with self.assertRaises(release.ReleaseError) as error:
                release.run(["xcodebuild", "-exportArchive"], log)
            self.assertIn("--allow-provisioning-updates", str(error.exception))
            self.assertIn("--resume", str(error.exception))
            self.assertNotIn("private-secret", str(error.exception))


class SimulatorFailureTests(ReleaseTestCase):
    launch_failure = ('Finished with error: Simulator device failed to launch com.apetrikov.b11k. '
                      'Underlying Error: Busy ("Application failed preflight checks"); '
                      'SimCallingSelector=launchApplicationWithID:options:pid:error:\n')

    def inventory(self, state="Shutdown"):
        return {"com.apple.CoreSimulator.SimRuntime.iOS-26-5": [
            {"name": "iPhone 17 Pro", "udid": "selected-device", "isAvailable": True, "state": state}]}

    def test_current_launch_failure_gets_an_actionable_resume_hint(self):
        log = self.root / "tests.log"

        def failed(command, **kwargs):
            kwargs["stdout"].write(self.launch_failure + 'private-secret\n')
            return Mock(returncode=65)

        command = release.test_command(arguments(self.output), "ios")
        with patch.object(release.subprocess, "run", side_effect=failed) as run:
            with self.assertRaisesRegex(release.SimulatorLaunchError, "simulator could not launch") as error:
                release.run(command, log)
        self.assertEqual(run.call_count, 1)
        self.assertIn("--resume", str(error.exception))
        self.assertIn("--ios-test-destination", str(error.exception))
        self.assertNotIn("private-secret", str(error.exception))

    def test_old_simulator_failure_does_not_mask_a_new_test_failure(self):
        log = self.root / "tests.log"
        log.write_text('Simulator device failed to launch. Application failed preflight checks\n')

        def failed(command, **kwargs):
            kwargs["stdout"].write("Test assertion failed\n")
            return Mock(returncode=65)

        with patch.object(release.subprocess, "run", side_effect=failed):
            with self.assertRaises(release.ReleaseError) as error:
                release.run(release.test_command(arguments(self.output), "ios"), log)
        self.assertNotIn("simulator could not launch", str(error.exception))
        self.assertNotIsInstance(error.exception, release.SimulatorLaunchError)

    def test_only_preflight_busy_before_test_execution_is_retryable(self):
        self.assertTrue(release.retryable_simulator_launch(self.launch_failure))
        for output in [
            'Test Case example failed', 'Test Suite B11kTests started',
            'Test run started.', 'Test example() failed after 0.1 seconds',
            'Test assertion failed', 'Expectation failed',
            '/tmp/App.swift:10:2: error: no such module', 'error: compilation failed',
        ]:
            with self.subTest(output=output):
                self.assertFalse(release.retryable_simulator_launch(output + '\n' + self.launch_failure))
        self.assertFalse(release.retryable_simulator_launch(self.launch_failure.replace('Busy', 'Denied')))

    def test_earlier_assertion_in_a_long_attempt_prevents_retry(self):
        log = self.root / 'tests.log'

        def failed(command, **kwargs):
            kwargs['stdout'].write('Test Case example failed\n' + ('diagnostics\n' * 7000) + self.launch_failure)
            return Mock(returncode=65)

        with patch.object(release.subprocess, 'run', side_effect=failed):
            with self.assertRaises(release.ReleaseError) as error:
                release.run(release.test_command(arguments(self.output), 'ios'), log)
        self.assertNotIsInstance(error.exception, release.SimulatorLaunchError)

    def test_selection_resolves_latest_available_runtime_and_explicit_id(self):
        devices = self.inventory()
        devices['com.apple.CoreSimulator.SimRuntime.iOS-25-0'] = [
            {'name': 'iPhone 17 Pro', 'udid': 'older-device', 'isAvailable': True}]
        devices['com.apple.CoreSimulator.SimRuntime.iOS-27-0'] = [
            {'name': 'iPhone 17 Pro', 'udid': 'unavailable-device', 'isAvailable': False}]
        selected, destination = release.select_test_simulator('platform=iOS Simulator,name=iPhone 17 Pro', devices)
        self.assertEqual(selected['udid'], 'selected-device')
        self.assertEqual(destination, 'platform=iOS Simulator,id=selected-device')
        selected, destination = release.select_test_simulator('platform=iOS Simulator,id=older-device,arch=arm64', devices)
        self.assertEqual(destination, 'platform=iOS Simulator,id=older-device,arch=arm64')
        selected, _ = release.select_test_simulator('platform=iOS Simulator,name=iPhone 17 Pro,OS=25.0', devices)
        self.assertEqual(selected['udid'], 'older-device')

    def test_ambiguous_or_unavailable_destination_fails_before_boot(self):
        devices = self.inventory()
        with self.assertRaisesRegex(release.ReleaseError, 'No available simulator'):
            release.select_test_simulator('platform=iOS Simulator,id=missing-device', devices)
        devices['com.apple.CoreSimulator.SimRuntime.iOS-26-5'].append(
            {'name': 'iPhone 17 Pro', 'udid': 'duplicate-device', 'isAvailable': True})
        with self.assertRaisesRegex(release.ReleaseError, 'Multiple simulators'):
            release.select_test_simulator('platform=iOS Simulator,name=iPhone 17 Pro', devices)

    def test_preparation_waits_for_boot_and_pins_the_test_destination(self):
        args = arguments(self.output)
        with patch.object(release, 'list_test_simulators', return_value=self.inventory()), \
             patch.object(release, 'run') as run:
            self.assertEqual(release.prepare_test_simulator(args, 'ios'), 'selected-device')
        run.assert_called_once_with(['xcrun', 'simctl', 'bootstatus', 'selected-device', '-b'],
                                    args.output_dir / 'logs/ios-simulator.log', timeout=180)
        command = release.test_command(args, 'ios')
        self.assertEqual(command[command.index('-destination') + 1], 'platform=iOS Simulator,id=selected-device')

    def test_busy_runner_reboots_only_selected_device_and_retries_once(self):
        for state, expected in [('Booted', ['test', 'shutdown', 'bootstatus', 'test']),
                                ('Shutdown', ['test', 'bootstatus', 'test'])]:
            with self.subTest(state=state):
                events = []

                def run(command, *args, **kwargs):
                    events.append('test' if command[-1] == 'test' else command[2])
                    if len(events) == 1:
                        raise release.SimulatorLaunchError('Busy')
                    if command[0] == 'xcrun':
                        self.assertEqual(command[3], 'selected-device')

                with patch.object(release, 'prepare_test_simulator', return_value='selected-device'), \
                     patch.object(release, 'list_test_simulators', return_value=self.inventory(state)), \
                     patch.object(release, 'run', side_effect=run):
                    release.run_tests(arguments(self.output), 'ios')
                self.assertEqual(events, expected)

    def test_assertion_failure_is_never_retried(self):
        with patch.object(release, 'prepare_test_simulator', return_value='selected-device'), \
             patch.object(release, 'list_test_simulators') as inventory, \
             patch.object(release, 'run', side_effect=release.ReleaseError('assertion failed')) as run:
            with self.assertRaisesRegex(release.ReleaseError, 'assertion failed'):
                release.run_tests(arguments(self.output), 'ios')
        self.assertEqual(run.call_count, 1)
        inventory.assert_not_called()

    def test_standalone_coverage_can_include_ui_tests(self):
        args = arguments(self.output)
        args.include_ui_tests = True
        args.test_result_bundle = self.output / 'tests.xcresult'
        command = release.test_command(args, 'ios')
        self.assertIn('-only-testing:B11kUITests', command)
        self.assertEqual(command[command.index('-resultBundlePath') + 1], args.test_result_bundle)
        self.assertEqual(command[command.index('-enableCodeCoverage') + 1], 'YES')

    def test_busy_runner_preserves_failed_bundle_before_retry(self):
        args = arguments(self.output)
        args.test_result_bundle = self.output / 'tests.xcresult'
        args.test_result_bundle.mkdir(parents=True)
        with patch.object(release, 'prepare_test_simulator', return_value='selected-device'), \
             patch.object(release, 'list_test_simulators', return_value=self.inventory()), \
             patch.object(release, 'run', side_effect=[release.SimulatorLaunchError('Busy'), None, None, None]):
            release.run_tests(args, 'ios')
        self.assertFalse(args.test_result_bundle.exists())
        self.assertTrue((self.output / 'tests-launch-failed.xcresult').is_dir())

    def test_boot_failure_stops_before_running_tests(self):
        with patch.object(release, 'list_test_simulators', return_value=self.inventory()), \
             patch.object(release, 'run', side_effect=release.ReleaseError('boot failed')) as run:
            with self.assertRaisesRegex(release.ReleaseError, 'boot failed'):
                release.run_tests(arguments(self.output), 'ios')
        self.assertEqual(run.call_count, 1)
        self.assertEqual(run.call_args.args[0][2], 'bootstatus')

    def test_boot_timeout_reports_its_log(self):
        log = self.root / 'simulator.log'
        with patch.object(release.subprocess, 'run', side_effect=subprocess.TimeoutExpired('xcrun', 180)):
            with self.assertRaisesRegex(release.ReleaseError, 'Command timed out') as error:
                release.run(['xcrun', 'simctl', 'bootstatus', 'selected-device', '-b'], log, timeout=180)
        self.assertIn(str(log), str(error.exception))

    def test_destination_override_runs_serially_with_test_gate_intact(self):
        destination = "platform=iOS Simulator,id=selected-device"
        args = arguments(self.output, "--ios-test-destination", destination)
        command = release.test_command(args, "ios")
        self.assertEqual(command[command.index("-destination") + 1], destination)
        self.assertEqual(command[command.index("-parallel-testing-enabled") + 1], "NO")
        self.assertIn("-only-testing:B11kTests", command)
        self.assertEqual(command[-1], "test")


class EnvFileTests(ReleaseTestCase):
    def setUp(self):
        super().setUp()
        self.config = self.root / "credentials with spaces"
        self.config.mkdir()
        self.key = self.config / "AuthKey_test #1.p8"
        self.key.write_text("fixture key; never used to sign")
        self.env_file = self.config / "apple.env"
        self.env_file.write_text('ASC_KEY_PATH="AuthKey_test #1.p8"\nASC_KEY_ID=file-key\nASC_ISSUER_ID=file-issuer\n')

    def test_dotenv_quotes_exports_comments_and_relative_key_path(self):
        self.env_file.write_bytes(b'\xef\xbb\xbf# Apple credentials\r\n\r\n'
            b'export ASC_KEY_PATH = "AuthKey_test #1.p8" # relative to this file\r\n'
            b"ASC_KEY_ID='file-key'\r\nASC_ISSUER_ID=file-issuer\r\nUNRELATED_SETTING=ignored\r\n")
        self.assertEqual(release.credentials(env_file=self.env_file), [str(self.key.resolve()), "file-key", "file-issuer"])

    def test_explicit_file_overrides_shell_without_changing_environment(self):
        ambient = {"ASC_KEY_PATH": "shell.p8", "ASC_KEY_ID": "shell-key", "ASC_ISSUER_ID": "shell-issuer"}
        with patch.dict(release.os.environ, ambient):
            self.assertEqual(release.credentials(required=True, env_file=self.env_file),
                             [str(self.key.resolve()), "file-key", "file-issuer"])
            self.assertEqual({name: release.os.environ[name] for name in ambient}, ambient)

    def test_incomplete_file_cannot_mix_in_shell_credentials(self):
        self.env_file.write_text('ASC_KEY_PATH="AuthKey_test #1.p8"\nASC_KEY_ID=file-key\n')
        with patch.dict(release.os.environ, {"ASC_ISSUER_ID": "shell-issuer"}), \
             self.assertRaisesRegex(release.ReleaseError, "--env-file must provide"):
            release.credentials(env_file=self.env_file)

    def test_invalid_file_errors_never_echo_secret_values(self):
        for contents, message in [
            ('ASC_KEY_ID="private-secret', "Invalid quoting"),
            ('ASC_KEY_ID=private-secret other', "Quote values"),
            ('ASC_KEY_ID=private-secret\nASC_KEY_ID=duplicate', "Duplicate"),
            ('private-secret malformed line', "Invalid assignment"),
        ]:
            with self.subTest(message=message):
                self.env_file.write_text(contents)
                with self.assertRaisesRegex(release.ReleaseError, message) as error:
                    release.credentials(env_file=self.env_file)
                self.assertNotIn("private-secret", str(error.exception))

    def test_missing_env_file_and_key_fail_clearly(self):
        with self.assertRaisesRegex(release.ReleaseError, "Cannot read --env-file"):
            release.credentials(env_file=self.config / "missing.env")
        self.key.unlink()
        with self.assertRaisesRegex(release.ReleaseError, "ASC_KEY_PATH does not point"):
            release.credentials(env_file=self.env_file)

    def test_shell_expressions_remain_literal_and_never_execute(self):
        marker = self.root / "should-not-exist"
        expression = "$(touch " + str(marker) + ")"
        self.env_file.write_text('ASC_KEY_PATH="AuthKey_test #1.p8"\nASC_KEY_ID="' + expression + '"\nASC_ISSUER_ID="${HOME}"\n')
        with patch.object(release.subprocess, "run", side_effect=AssertionError("shell execution")):
            creds = release.credentials(env_file=self.env_file)
        self.assertEqual(creds[1:], [expression, "${HOME}"])
        self.assertFalse(marker.exists())

    def test_release_passes_file_credentials_to_pipeline(self):
        args = arguments(self.output, "--env-file", str(self.env_file), "--distribute", "testflight", "--signing-auth", "xcode")
        with patch.object(release.sys, "platform", "darwin"), patch.object(release.shutil, "which", return_value="tool"), \
             patch.object(release, "execute_release") as execute:
            release.release(args)
        execute.assert_called_once_with(args, [str(self.key.resolve()), "file-key", "file-issuer"], True)

    def test_dry_run_validates_explicit_file_and_redacts_values(self):
        args = arguments(self.output, "--env-file", str(self.env_file), "--distribute", "testflight", "--dry-run")
        output = io.StringIO()
        with redirect_stdout(output), patch.object(release.subprocess, "run", side_effect=AssertionError("process started")), \
             patch.object(release, "AppStoreConnect", side_effect=AssertionError("API created")):
            release.release(args)
        for value in ("file-key", "file-issuer", self.key.name):
            self.assertNotIn(value, output.getvalue())
        self.assertFalse(self.output.exists())
        self.env_file.write_text("ASC_KEY_ID=only-one-value")
        with self.assertRaisesRegex(release.ReleaseError, "--env-file must provide"):
            release.release(args)


class PipelineTests(ReleaseTestCase):
    def pipeline(self, failure=None, existing=None):
        self.output.mkdir()
        events, builds = [], dict(existing or {})
        fingerprint = ["source-a"]
        api = Mock()
        api.find_build.side_effect = lambda app, args, platform: builds.get(platform)

        def processed(app, args, platform):
            events.append(("processed", platform))
            return builds[platform]

        api.wait_for_build.side_effect = processed

        def run(command, log=None, **kwargs):
            command = list(map(str, command))
            if command[-1] == "test":
                kind, platform = "test", "ios"
            elif command[-1] == "archive":
                kind, platform = "archive", "ios"
            elif "-exportArchive" in command:
                kind = Path(command[command.index("-exportPath") + 1]).parent.name
                platform = Path(command[command.index("-exportPath") + 1]).name
            else:
                return
            events.append((kind, platform))
            if failure:
                failure(kind, platform, fingerprint)
            if kind == "upload":
                builds[platform] = {"id": platform + "-build", "attributes": {"processingState": "VALID"}}

        stack = self.stack
        stack.enter_context(patch.object(release, "run", side_effect=run))
        stack.enter_context(patch.object(release, "prepare_test_simulator", return_value="selected-device"))
        stack.enter_context(patch.object(release, "source_fingerprint", side_effect=lambda: fingerprint[0]))
        stack.enter_context(patch.object(release, "verify_archive"))
        stack.enter_context(patch.object(release, "AppStoreConnect", return_value=api))
        stack.enter_context(patch.object(release, "preflight_distribution", return_value=("app", [])))
        stack.enter_context(patch.object(release, "distribute_testflight", side_effect=lambda api, build, groups: events.append(("distribute", build))))
        return arguments(self.output, "--distribute", "testflight"), events, builds, fingerprint

    def execute(self, args):
        release.execute_release(args, ["", "", ""], online=True)

    def test_archive_and_export_precede_upload_and_processing_precedes_distribution(self):
        args, events, _, _ = self.pipeline()
        self.execute(args)
        self.assertEqual(events, [("test", "ios"), ("archive", "ios"),
            ("export", "ios"), ("upload", "ios"), ("processed", "ios"), ("distribute", "ios-build")])
        record = json.loads((self.output / "release.json").read_text())
        self.assertEqual(record["status"], "complete")
        args.resume = True
        events.clear()
        self.execute(args)
        self.assertFalse(any(kind in ("test", "archive", "export", "upload") for kind, _ in events))


    def test_local_export_failure_never_uploads(self):
        def fail(kind, platform, fingerprint):
            if (kind, platform) == ("export", "ios"):
                raise release.ReleaseError("export failed")
        args, events, _, _ = self.pipeline(fail)
        with self.assertRaises(release.ReleaseError):
            self.execute(args)
        self.assertFalse(any(kind == "upload" for kind, _ in events))

    def test_archive_failure_resumes_after_completed_tests_without_uploading(self):
        fail_archive = [True]

        def fail(kind, platform, fingerprint):
            if kind == "archive" and fail_archive[0]:
                raise release.ReleaseError("archive failed")

        args, events, _, _ = self.pipeline(fail)
        with self.assertRaisesRegex(release.ReleaseError, "archive failed"):
            self.execute(args)
        self.assertEqual(events, [("test", "ios"), ("archive", "ios")])
        fail_archive[0], args.resume = False, True
        events.clear()
        self.execute(args)
        self.assertEqual(events[0], ("archive", "ios"))
        self.assertNotIn(("test", "ios"), events)

    def test_test_failure_never_archives_or_uploads(self):
        def fail(kind, platform, fingerprint):
            if kind == "test":
                raise release.ReleaseError("tests failed")

        args, events, _, _ = self.pipeline(fail)
        with self.assertRaisesRegex(release.ReleaseError, "tests failed"):
            self.execute(args)
        self.assertEqual(events, [("test", "ios")])

    def test_unknown_existing_build_fails_before_building(self):
        args, events, _, _ = self.pipeline(existing={"ios": {"id": "existing"}})
        with self.assertRaisesRegex(release.ReleaseError, "already exists"):
            self.execute(args)
        self.assertEqual(events, [])

    def test_repeated_simulator_launch_failure_stops_before_archive_or_upload(self):
        def fail(kind, platform, fingerprint):
            if kind == 'test':
                raise release.SimulatorLaunchError('preflight Busy')

        args, events, _, _ = self.pipeline(fail)
        devices = {'com.apple.CoreSimulator.SimRuntime.iOS-26-5': [
            {'name': 'iPhone 17 Pro', 'udid': 'selected-device', 'isAvailable': True, 'state': 'Booted'}]}
        with patch.object(release, 'list_test_simulators', return_value=devices):
            with self.assertRaisesRegex(release.SimulatorLaunchError, 'preflight Busy'):
                self.execute(args)
        self.assertEqual(events, [('test', 'ios'), ('test', 'ios')])
        record = json.loads((self.output / 'release.json').read_text())
        self.assertEqual(record['status'], 'failed')
        self.assertFalse(record['platforms']['ios'].get('tested'))

    def test_uncertain_upload_requires_explicit_retry_or_matching_apple_build(self):
        def fail(kind, platform, fingerprint):
            if kind == "upload" and platform == "ios":
                raise release.ReleaseError("connection lost")
        args, events, builds, _ = self.pipeline(fail)
        with self.assertRaisesRegex(release.ReleaseError, "connection lost"):
            self.execute(args)
        args.resume = True
        events.clear()
        with self.assertRaisesRegex(release.ReleaseError, "outcome is uncertain"):
            self.execute(args)
        self.assertEqual(events, [])
        builds["ios"] = {"id": "ios-build", "attributes": {"processingState": "VALID"}}
        self.execute(args)
        self.assertFalse(any(kind == "upload" for kind, _ in events))

    def test_changed_sources_during_build_permanently_block_resume(self):
        def changed(kind, platform, fingerprint):
            if (kind, platform) == ("archive", "ios"):
                fingerprint[0] = "source-b"
        args, events, _, fingerprint = self.pipeline(changed)
        with self.assertRaisesRegex(release.ReleaseError, "changed during the build"):
            self.execute(args)
        fingerprint[0] = "source-a"
        args.resume = True
        events.clear()
        with self.assertRaisesRegex(release.ReleaseError, "changed during this release"):
            self.execute(args)
        self.assertEqual(events, [])

    def test_resume_rejects_changed_configuration(self):
        args, _, _, _ = self.pipeline()
        self.execute(args)
        args.resume, args.build = True, "77"
        with self.assertRaisesRegex(release.ReleaseError, "parameters do not match"):
            self.execute(args)

    def test_missing_compliance_preserves_upload_for_resume(self):
        original_distribution = release.distribute_testflight
        args, events, _, _ = self.pipeline()
        args.version, args.build = "0.14", "78"

        def check(api, build_id, groups):
            if build_id == "ios-build":
                detail_api = Mock()
                detail_api.request.return_value = {"data": {"attributes": {"internalBuildState": "MISSING_EXPORT_COMPLIANCE"}}}
                original_distribution(detail_api, build_id, groups)

        # The pipeline helper mocks distribution; use the real function to exercise the error path.
        with patch.object(release, "distribute_testflight", side_effect=check):
            with self.assertRaisesRegex(release.ReleaseError, r"ios 0\.14 \(78\):.*MISSING_EXPORT_COMPLIANCE") as error:
                self.execute(args)
        self.assertIn("Provide Export Compliance Information", str(error.exception))
        self.assertIn("already-uploaded build", str(error.exception))
        self.assertIn("--resume", str(error.exception))
        manifest = json.loads((self.output / "release.json").read_text())
        self.assertTrue(manifest["platforms"]["ios"]["uploaded"])
        self.assertEqual(manifest["platforms"]["ios"]["build_id"], "ios-build")
        events.clear()
        args.resume = True
        self.execute(args)
        self.assertFalse(any(kind in ("test", "archive", "export", "upload") for kind, _ in events))


if __name__ == "__main__":
    unittest.main()
