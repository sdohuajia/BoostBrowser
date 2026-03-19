import PyInstaller.__main__
import os
import sys

# Define base path
base_dir = os.path.dirname(os.path.abspath(__file__))

# Define packaging options
params = [
    'main.py',              # Main entry point
    '--name=AntigravityAds', # EXE Name
    '--onefile',            # Bundle into a single EXE
    '--noconsole',          # Hide the black terminal window
    '--clean',              # Clean cache before build
    # Add hidden imports if necessary
    '--hidden-import=PyQt6.QtCore',
    '--hidden-import=PyQt6.QtGui',
    '--hidden-import=PyQt6.QtWidgets',
    '--hidden-import=pynput.keyboard._win32',
    '--hidden-import=pynput.mouse._win32',
    '--hidden-import=pygetwindow',
    '--hidden-import=win32gui',
    '--hidden-import=win32process',
    '--hidden-import=faker',
    '--hidden-import=socks',
    '--collect-all=faker', # Ensure faker data (locales) are included
    '--add-data=app.png;.', # Include the icon file
]

print(f"Starting build for {params[1]}...")
PyInstaller.__main__.run(params)
print("Build complete! Check the 'dist' folder for your EXE.")
