"""Tests to verify Python files have valid syntax and structure."""
import ast
import os
import sys


def test_sensor_syntax():
    """Test sensor.py has valid Python syntax."""
    path = os.path.join(os.path.dirname(__file__), '..', 'sensor.py')
    with open(path, 'r') as f:
        source = f.read()
    ast.parse(source)


def test_binary_sensor_syntax():
    """Test binary_sensor.py has valid Python syntax."""
    path = os.path.join(os.path.dirname(__file__), '..', 'binary_sensor.py')
    with open(path, 'r') as f:
        source = f.read()
    ast.parse(source)


def test_config_flow_syntax():
    """Test config_flow.py has valid Python syntax."""
    path = os.path.join(os.path.dirname(__file__), '..', 'config_flow.py')
    with open(path, 'r') as f:
        source = f.read()
    ast.parse(source)


def test_const_syntax():
    """Test const.py has valid Python syntax."""
    path = os.path.join(os.path.dirname(__file__), '..', 'const.py')
    with open(path, 'r') as f:
        source = f.read()
    ast.parse(source)


def test_sensor_has_smartd_classes():
    """Test sensor.py contains expected Smartd classes."""
    path = os.path.join(os.path.dirname(__file__), '..', 'sensor.py')
    with open(path, 'r') as f:
        source = f.read()
    assert 'SmartdAlertsSensor' in source
    assert 'SmartdDeviceSensor' in source


def test_binary_sensor_has_smartd_class():
    """Test binary_sensor.py contains SmartdAlertBinarySensor."""
    path = os.path.join(os.path.dirname(__file__), '..', 'binary_sensor.py')
    with open(path, 'r') as f:
        source = f.read()
    assert 'SmartdAlertBinarySensor' in source


def test_const_has_notify_service():
    """Test const.py contains CONF_NOTIFY_SERVICE."""
    path = os.path.join(os.path.dirname(__file__), '..', 'const.py')
    with open(path, 'r') as f:
        source = f.read()
    assert 'CONF_NOTIFY_SERVICE' in source


def test_config_flow_has_notify_service():
    """Test config_flow.py references notify_service."""
    path = os.path.join(os.path.dirname(__file__), '..', 'config_flow.py')
    with open(path, 'r') as f:
        source = f.read()
    assert 'CONF_NOTIFY_SERVICE' in source
