#!/usr/bin/env python3
"""Verify installed C/C++ consumers and curl config-to-module compatibility."""
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def run(*args):
    subprocess.run([str(arg) for arg in args], check=True)


def config_to_module_regression(work, prefix, consumer, configure):
    """Reproduce curl's real-library alias export, then consume with FindCURL."""
    fixture = work / 'curl-config-fixture'
    fixture.mkdir()
    # FindCURL supplies the actual local library and its transitive settings.
    # The fixture changes only target naming, matching curl's config package.
    (fixture / 'CMakeLists.txt').write_text('''cmake_minimum_required(VERSION 3.18)
project(curl_config_fixture LANGUAGES C)
set(CURL_NO_CURL_CMAKE TRUE)
find_package(CURL MODULE REQUIRED)
if(TARGET CURL::libcurl_shared OR TARGET CURL::libcurl_static)
  message(FATAL_ERROR "The fixture probe must discover curl through FindCURL")
endif()
set(config "${CMAKE_CURRENT_BINARY_DIR}/CURLConfig.cmake")
file(WRITE "${config}" "add_library(CURL::libcurl_shared UNKNOWN IMPORTED)\\n")
foreach(property IN ITEMS
    INTERFACE_INCLUDE_DIRECTORIES INTERFACE_SYSTEM_INCLUDE_DIRECTORIES
    INTERFACE_COMPILE_DEFINITIONS INTERFACE_COMPILE_OPTIONS
    INTERFACE_LINK_LIBRARIES INTERFACE_LINK_OPTIONS
    IMPORTED_LOCATION IMPORTED_IMPLIB IMPORTED_CONFIGURATIONS
    IMPORTED_LOCATION_RELEASE IMPORTED_LOCATION_DEBUG
    IMPORTED_LOCATION_RELWITHDEBINFO IMPORTED_LOCATION_MINSIZEREL
    IMPORTED_IMPLIB_RELEASE IMPORTED_IMPLIB_DEBUG
    IMPORTED_IMPLIB_RELWITHDEBINFO IMPORTED_IMPLIB_MINSIZEREL
    MAP_IMPORTED_CONFIG_RELEASE MAP_IMPORTED_CONFIG_DEBUG
    MAP_IMPORTED_CONFIG_RELWITHDEBINFO MAP_IMPORTED_CONFIG_MINSIZEREL)
  get_target_property(value CURL::libcurl ${property})
  if(NOT value STREQUAL "value-NOTFOUND")
    file(APPEND "${config}"
      "set_property(TARGET CURL::libcurl_shared PROPERTY ${property} [==[${value}]==])\\n")
  endif()
endforeach()
file(APPEND "${config}" "add_library(CURL::libcurl ALIAS CURL::libcurl_shared)\\nset(CURL_FOUND TRUE)\\n")
''')
    probe = work / 'curl-probe'
    curl_root = prefix / 'third_party/curl'
    curl_args = ['-DCURL_ROOT=' + str(curl_root)] if curl_root.is_dir() else []
    run(*configure(fixture, probe), *curl_args)

    build = work / 'config-builder'
    installed = work / 'config-install'
    run(*configure(ROOT / 'sdk/native', build),
        '-DCMAKE_INSTALL_PREFIX=' + str(installed),
        '-DCURL_DIR=' + str(probe), '-DCURL_NO_CURL_CMAKE=FALSE', *curl_args)
    run('cmake', '--build', build, '--config', 'Release')
    run('cmake', '--install', build, '--config', 'Release')
    exports = list(installed.rglob('LicentivoTargets.cmake'))
    if len(exports) != 1 or 'CURL::libcurl_shared' not in exports[0].read_text():
        raise ValueError('The regression build did not export the curl config alias.')
    if curl_root.is_dir():
        shutil.copytree(curl_root, installed / 'third_party/curl', dirs_exist_ok=True)
    relocated = work / 'config-install-relocated'
    installed.rename(relocated)
    configs = list(relocated.rglob('LicentivoConfig.cmake'))
    if len(configs) != 1:
        raise ValueError('Expected one installed upstream package config.')
    config = configs[0]
    original = config.read_text()
    include = 'include("${CMAKE_CURRENT_LIST_DIR}/LicentivoTargets.cmake")'
    if original.count(include) != 1:
        raise ValueError('Cannot prepare the old-behavior negative control.')

    def module_command(output):
        return [*configure(consumer, output), '-DCMAKE_PREFIX_PATH=' + str(relocated),
                '-DCMAKE_FIND_PACKAGE_PREFER_CONFIG=FALSE', '-DCURL_NO_CURL_CMAKE=TRUE',
                '-DLAUNCH_REQUIRE_CURL_MODULE=ON']

    # Prove this combination detects the original defect. Only the temporary
    # installation is changed; restore it before the positive consumer check.
    try:
        config.write_text(original.replace(include, include + '\nreturn()'))
        result = subprocess.run(module_command(work / 'old-behavior-consumer'),
                                text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        if result.returncode == 0 or 'CURL::libcurl_shared' not in result.stdout:
            raise ValueError('The negative control did not reproduce the missing curl target.\n' + result.stdout)
    finally:
        config.write_text(original)
    print('Negative control reproduced the missing CURL::libcurl_shared target.', flush=True)
    output = work / 'config-to-module-consumer'
    run(*module_command(output))
    run('cmake', '--build', output, '--config', 'Release')
    execute_consumers(output)


def execute_consumers(build):
    for name in ('consumer_c', 'consumer_cpp'):
        executable = build / (name + ('.exe' if os.name == 'nt' else ''))
        run(executable)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--prefix', required=True, type=Path)
    parser.add_argument('--cc', type=Path)
    args = parser.parse_args()
    version = (ROOT / 'VERSION').read_text().strip()
    upstream = (ROOT / 'sdk/UPSTREAM_VERSION').read_text().strip()

    def configure(source, build):
        command = ['cmake', '-S', source, '-B', build, '-DCMAKE_BUILD_TYPE=Release']
        if args.cc:
            compiler = args.cc.resolve()
            command += ['-G', 'MinGW Makefiles', '-DCMAKE_C_COMPILER=' + str(compiler),
                        '-DCMAKE_CXX_COMPILER=' + str(compiler.parent / 'g++.exe'),
                        '-DCMAKE_MAKE_PROGRAM=' + str(compiler.parent / 'mingw32-make.exe')]
        return command

    with tempfile.TemporaryDirectory(prefix='launch-native-consumer-') as temporary:
        work = Path(temporary)
        (work / 'consumer.c').write_text('''#include <account.h>
int main(void) {
    cJSON *value = ln_json("{}");
    if (!value) return 1;
    cJSON_Delete(value);
    launch_application_close(NULL);
    return 0;
}
''')
        (work / 'consumer.cpp').write_text('''#include <launch.hpp>
int main() {
    cJSON *value = ln_json("{}");
    if (!value) return 1;
    cJSON_Delete(value);
    try { licentivo_launch::Application app(nullptr); }
    catch (const std::runtime_error&) { return 0; }
    return 1;
}
''')
        (work / 'CMakeLists.txt').write_text('''cmake_minimum_required(VERSION 3.18)
project(launch_consumer LANGUAGES C CXX)
find_package(LicentivoLaunch @VERSION@ EXACT CONFIG REQUIRED)
find_package(Licentivo @UPSTREAM@ EXACT CONFIG REQUIRED)
if(LAUNCH_REQUIRE_CURL_MODULE)
  if(NOT TARGET CURL::libcurl OR TARGET CURL::libcurl_shared OR TARGET CURL::libcurl_static)
    message(FATAL_ERROR "The consumer must have only FindCURL's public curl target")
  endif()
endif()
add_executable(consumer_c consumer.c)
licentivo_launch_link(consumer_c)
add_executable(consumer_cpp consumer.cpp)
licentivo_launch_link(consumer_cpp CXX)
'''.replace('@VERSION@', version).replace('@UPSTREAM@', upstream))
        for mode in ('default', 'module'):
            build = work / ('build-' + mode)
            command = [*configure(work, build), '-DCMAKE_PREFIX_PATH=' + str(args.prefix.resolve())]
            if mode == 'module':
                command += ['-DCURL_NO_CURL_CMAKE=TRUE', '-DLAUNCH_REQUIRE_CURL_MODULE=ON',
                            '-DCMAKE_FIND_PACKAGE_PREFER_CONFIG=FALSE']
            run(*command)
            run('cmake', '--build', build, '--config', 'Release')
            execute_consumers(build)
        config_to_module_regression(work, args.prefix.resolve(), work, configure)
    print('Installed Launch C/C++ consumers passed: default, module, and config-to-module regression.')


if __name__ == '__main__':
    main()
