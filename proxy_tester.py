import requests
import json
import time

def test_proxy(proxy_str, proxy_type='HTTP'):
    """
    Tests the connectivity of a proxy and returns (success, details)
    details: dict with 'ip', 'region', 'status_msg' or None
    """
    
    # Normalize proxy string
    proxy_str = proxy_str.strip()
    
    # If the string starts with a scheme, check if it matches proxy_type
    current_scheme = ""
    if "://" in proxy_str:
        current_scheme = proxy_str.split("://")[0].lower()
        # If user selected SOCKS5 but string has http://, we should probably warn or fix.
        # For now, let's just make sure we use the correct internal scheme for requests.
        if current_scheme == 'socks5': current_scheme = 'socks5h'
        proxy_url = proxy_str.replace(proxy_str.split("://")[0], current_scheme, 1)
    else:
        scheme = proxy_type.lower()
        if scheme == 'ssh':
            return False, {"status_msg": "SSH 代理测试暂不支持在线连通性检测", "ip": "", "region": ""}
        
        # requests expects socks5h or http
        if scheme == 'socks5': scheme = 'socks5h'
        if scheme == 'socks4': scheme = 'socks4'
        proxy_url = f"{scheme}://{proxy_str}"
    
    proxies = {
        "http": proxy_url,
        "https": proxy_url
    }
    
    test_urls = [
        "https://api.ipify.org?format=json",
        "https://ipinfo.io/json",
        "http://httpbin.org/ip"
    ]
    
    start_time = time.time()
    try:
        # Try a quick IP check
        response = requests.get(test_urls[0], proxies=proxies, timeout=10)
        elapsed = round(time.time() - start_time, 2)
        
        if response.status_code == 200:
            data = response.json()
            ip = data.get('ip', 'Unknown')
            
            # Try to get region info
            region = "未知地区"
            try:
                # Secondary call for more details if needed, or use the current IP
                geo_resp = requests.get(f"http://ip-api.com/json/{ip}", proxies=proxies, timeout=5)
                if geo_resp.status_code == 200:
                    geo_data = geo_resp.json()
                    region = f"{geo_data.get('country', '')} {geo_data.get('city', '')}".strip()
            except:
                pass
                
            return True, {
                "status_msg": f"连接成功 ({elapsed}s)",
                "ip": ip,
                "region": region
            }
        else:
            return False, {"status_msg": f"状态码异常: {response.status_code}", "ip": "", "region": ""}
            
    except requests.exceptions.Timeout:
        return False, {"status_msg": "连接超时", "ip": "", "region": ""}
    except requests.exceptions.ProxyError:
        return False, {"status_msg": "代理连接错误", "ip": "", "region": ""}
    except Exception as e:
        return False, {"status_msg": f"测试失败: {str(e)}", "ip": "", "region": ""}

if __name__ == "__main__":
    # Quick test
    # print(test_proxy("127.0.0.1:7890", "HTTP"))
    pass
