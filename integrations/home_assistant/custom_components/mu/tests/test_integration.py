"""Cross-cutting integration tests."""

import ast
import os


def _get_source_dir():
    return os.path.dirname(os.path.dirname(__file__))


def test_all_python_files_parse():
    """All Python source files should be valid Python."""
    src = _get_source_dir()
    py_files = [f for f in os.listdir(src) if f.endswith('.py')]
    for f in py_files:
        path = os.path.join(src, f)
        with open(path) as fh:
            ast.parse(fh.read())


def test_no_print_statements():
    """Source files should not have print() statements (use _LOGGER)."""
    src = _get_source_dir()
    exceptions = {'__pycache__'}
    for f in os.listdir(src):
        if not f.endswith('.py') or f in exceptions:
            continue
        path = os.path.join(src, f)
        with open(path) as fh:
            tree = ast.parse(fh.read())
        for node in ast.walk(tree):
            if isinstance(node, ast.Call):
                func = node.func
                if isinstance(func, ast.Name) and func.id == 'print':
                    assert False, f"Found print() in {f} line {node.lineno}"


def test_all_modules_have_docstrings():
    """Each Python module should have a docstring."""
    src = _get_source_dir()
    for f in os.listdir(src):
        if not f.endswith('.py') or f == '__init__.py':
            continue
        path = os.path.join(src, f)
        with open(path) as fh:
            tree = ast.parse(fh.read())
        docstring = ast.get_docstring(tree)
        assert docstring, f"{f} is missing a module docstring"


def test_no_wildcard_imports():
    """Source files should not use wildcard imports."""
    src = _get_source_dir()
    for f in os.listdir(src):
        if not f.endswith('.py'):
            continue
        path = os.path.join(src, f)
        with open(path) as fh:
            tree = ast.parse(fh.read())
        for node in ast.walk(tree):
            if isinstance(node, ast.ImportFrom):
                for alias in node.names:
                    assert alias.name != '*', f"Wildcard import in {f}: from {node.module} import *"


def test_www_directory_exists():
    """The www directory should exist with panel assets."""
    src = _get_source_dir()
    www = os.path.join(src, 'www')
    assert os.path.isdir(www), "www directory not found"
    assert os.path.isfile(os.path.join(www, 'mu-panel.js')), "mu-panel.js not found"


def test_platforms_constant():
    """PLATFORMS should list all entity platforms."""
    src = _get_source_dir()
    init_path = os.path.join(src, '__init__.py')
    with open(init_path) as f:
        tree = ast.parse(f.read())

    for node in ast.walk(tree):
        if isinstance(node, ast.Assign):
            for target in node.targets:
                if isinstance(target, ast.Name) and target.id == 'PLATFORMS':
                    platforms = [elt.value for elt in node.value.elts if isinstance(elt, ast.Constant)]
                    # Verify all platform modules exist
                    for platform in platforms:
                        platform_file = os.path.join(src, f"{platform}.py")
                        assert os.path.isfile(platform_file), f"Platform file {platform}.py not found"
                    return
    assert False, "PLATFORMS constant not found"
