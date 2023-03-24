"""
采集mongoshake增量复制信息，主要包含如下指标
    tps,
    延迟
"""
import boto3
import time
import json
import requests

TARGET_URLS = ['http://127.0.0.1:9100/repl']  # mongoshake 增量监控地址
INTERVAL = 60       # 采集间隔(秒)

def query_repl_result(url):
    resp = requests.get(url)
    result = json.loads(resp.content)
    return result


def parse_metrics(repl_result):
    tps = repl_result.get('tps', -1)
    who = repl_result.get('who')
    now_unix = repl_result.get('now').get('unix')
    lsn_ckpt_unix = repl_result.get('lsn_ckpt').get('unix')
    lag = now_unix - lsn_ckpt_unix  # lag in seconds
    storage_resolution = 1 if INTERVAL < 60 else 60     # 是否需要按秒级存储指标
    metric_data = [
        {
            'MetricName': 'tps',
            'Dimensions': [
                {
                    'Name': 'mongoshake-instance',
                    'Value': who
                },
            ],
            'Timestamp': now_unix,
            'Value': tps,
            'StorageResolution': storage_resolution
        },
        {
            'MetricName': 'lag',
            'Dimensions': [
                {
                    'Name': 'mongoshake-instance',
                    'Value': who
                },
            ],
            'Timestamp': now_unix,
            'Value': lag,
            'Unit': 'Seconds',
            'StorageResolution': storage_resolution
        }
    ]
    return metric_data


def put_metrics_to_cloudwatch(repl_metrics):
    cw_client = boto3.client('cloudwatch')
    cw_client.put_metric_data(Namespace='mongoshake', MetricData=repl_metrics)


def main():
    while True:
        time.sleep(INTERVAL)
        for url in TARGET_URLS:
            repl_result = query_repl_result(url)
            metrics = parse_metrics(repl_result)
            put_metrics_to_cloudwatch(metrics)
        

if __name__ == '__main__':
    main()
