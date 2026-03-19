import sqlite3
import os

import sys

def get_db_path():
    # If running as a bundled EXE, use the EXE's directory
    if getattr(sys, 'frozen', False):
        base_dir = os.path.dirname(sys.executable)
    else:
        # If running as a script, use the script's directory
        base_dir = os.path.dirname(os.path.abspath(__file__))
    return os.path.join(base_dir, 'profiles.db')

DB_PATH = get_db_path()

def get_connection():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    return conn

def init_db():
    conn = get_connection()
    cursor = conn.cursor()
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS profiles (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            notes TEXT,
            proxy TEXT,
            user_agent TEXT,
            chrome_version TEXT DEFAULT '134',
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    ''')
    cursor.execute('''
        CREATE TABLE IF NOT EXISTS proxies (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            proxy_str TEXT NOT NULL UNIQUE,
            type TEXT DEFAULT 'HTTP',
            notes TEXT,
            last_status TEXT,
            last_check_at TIMESTAMP,
            region TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    ''')
    conn.commit()
    
    # Simple migration for existing DBs
    try:
        cursor.execute("ALTER TABLE profiles ADD COLUMN chrome_version TEXT DEFAULT '134'")
    except: pass
    try:
        cursor.execute("ALTER TABLE proxies ADD COLUMN type TEXT DEFAULT 'HTTP'")
    except: pass
    try:
        cursor.execute("ALTER TABLE proxies ADD COLUMN last_status TEXT")
    except: pass
    try:
        cursor.execute("ALTER TABLE proxies ADD COLUMN last_check_at TIMESTAMP")
    except: pass
    try:
        cursor.execute("ALTER TABLE proxies ADD COLUMN region TEXT")
    except: pass
    conn.commit()
    conn.close()

def get_all_profiles():
    conn = get_connection()
    cursor = conn.cursor()
    cursor.execute('SELECT id, name, notes, proxy, user_agent, chrome_version, created_at FROM profiles')
    rows = cursor.fetchall()
    conn.close()
    
    profiles = []
    for row in rows:
        profiles.append({
            'id': row[0],
            'name': row[1],
            'notes': row[2],
            'proxy': row[3],
            'user_agent': row[4],
            'chrome_version': row[5],
            'created_at': row[6]
        })
    return profiles

def add_profile(name, notes='', proxy='', user_agent='', chrome_version='134'):
    conn = get_connection()
    cursor = conn.cursor()
    cursor.execute('''
        INSERT INTO profiles (name, notes, proxy, user_agent, chrome_version)
        VALUES (?, ?, ?, ?, ?)
    ''', (name, notes, proxy, user_agent, chrome_version))
    conn.commit()
    profile_id = cursor.lastrowid
    conn.close()
    return profile_id

def update_profile(profile_id, name, notes, proxy, user_agent, chrome_version):
    conn = get_connection()
    cursor = conn.cursor()
    cursor.execute('''
        UPDATE profiles
        SET name = ?, notes = ?, proxy = ?, user_agent = ?, chrome_version = ?
        WHERE id = ?
    ''', (name, notes, proxy, user_agent, chrome_version, profile_id))
    conn.commit()
    conn.close()

def delete_profile(profile_id):
    conn = get_connection()
    cursor = conn.cursor()
    cursor.execute('DELETE FROM profiles WHERE id = ?', (profile_id,))
    conn.commit()
    conn.close()

# Proxy Management
def get_all_proxies():
    conn = get_connection()
    cursor = conn.cursor()
    cursor.execute('SELECT id, proxy_str, type, notes, last_status, last_check_at, region, created_at FROM proxies')
    rows = cursor.fetchall()
    conn.close()
    
    proxies = []
    for row in rows:
        proxies.append({
            'id': row[0],
            'proxy_str': row[1],
            'type': row[2],
            'notes': row[3],
            'last_status': row[4],
            'last_check_at': row[5],
            'region': row[6],
            'created_at': row[7]
        })
    return proxies

def add_proxy(proxy_str, proxy_type='HTTP', notes=''):
    conn = get_connection()
    cursor = conn.cursor()
    try:
        cursor.execute('''
            INSERT INTO proxies (proxy_str, type, notes)
            VALUES (?, ?, ?)
        ''', (proxy_str, proxy_type, notes))
        conn.commit()
    except sqlite3.IntegrityError:
        pass # Ignore duplicates
    finally:
        conn.close()

def update_proxy(proxy_id, proxy_str, proxy_type, notes):
    conn = get_connection()
    cursor = conn.cursor()
    cursor.execute('''
        UPDATE proxies
        SET proxy_str = ?, type = ?, notes = ?
        WHERE id = ?
    ''', (proxy_str, proxy_type, notes, proxy_id))
    conn.commit()
    conn.close()

def update_proxy_status(proxy_id, status, region):
    conn = get_connection()
    cursor = conn.cursor()
    import datetime
    now = datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    cursor.execute('''
        UPDATE proxies
        SET last_status = ?, region = ?, last_check_at = ?
        WHERE id = ?
    ''', (status, region, now, proxy_id))
    conn.commit()
    conn.close()

def delete_proxy(proxy_id):
    conn = get_connection()
    cursor = conn.cursor()
    cursor.execute('DELETE FROM proxies WHERE id = ?', (proxy_id,))
    conn.commit()
    conn.close()

if __name__ == '__main__':
    init_db()
    print("Database initialized successfully.")
