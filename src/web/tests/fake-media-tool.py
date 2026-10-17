#!/usr/bin/env python3
"""E2E-only process fixture; never loaded by the production configuration."""
import pathlib
import json
import sys
import time
import shutil

if '--version' in sys.argv or '-version' in sys.argv:
    print('test-fixture-tool 1.0')
    raise SystemExit(0)
if '-frames:v' in sys.argv:
    assert '-protocol_whitelist' in sys.argv
    assert sys.argv[sys.argv.index('-protocol_whitelist') + 1] == 'file,pipe'
    assert '-ss' in sys.argv and '-an' in sys.argv
    position = float(sys.argv[sys.argv.index('-ss') + 1])
    if position > 12:
        raise SystemExit(1)
    shutil.copyfile(pathlib.Path(__file__).parent / 'fixtures' / 'frame.jpg', sys.argv[-1])
    raise SystemExit(0)
if '--dump-single-json' in sys.argv and '--no-plugin-dirs' in sys.argv and '--simulate' in sys.argv:
    from urllib.parse import urlparse, parse_qs
    target = urlparse(sys.argv[-1])
    video_id = parse_qs(target.query).get('v', [target.path.rstrip('/').split('/')[-1]])[0]
    if 'BVfailed0000' in video_id:
        print('ERROR: HTTP 412 test platform restriction', file=sys.stderr)
        raise SystemExit(1)
    if video_id == 'playlist':
        print(json.dumps({'_type': 'playlist', 'title': '测试播放列表', 'entries': []}))
        raise SystemExit(0)
    audio_only = video_id.endswith('.mp3')
    formats = [
        {'format_id': '137', 'ext': 'mp4', 'height': 1080, 'width': 1920, 'fps': 30, 'vcodec': 'avc1', 'acodec': 'none', 'filesize': 10240000},
        {'format_id': '136', 'ext': 'mp4', 'height': 720, 'width': 1280, 'fps': 30, 'vcodec': 'avc1', 'acodec': 'none'},
        {'format_id': '140', 'ext': 'm4a', 'vcodec': 'none', 'acodec': 'aac', 'abr': 128},
    ]
    print(json.dumps({
        'id': video_id, 'title': video_id if video_id.startswith('BV') else '下载测试视频',
        'uploader': '测试作者', 'extractor_key': 'Generic', 'duration': 62,
        'thumbnail': 'https://fixture.example/cover.jpg',
        'formats': formats[-1:] if audio_only else formats,
        'subtitles': {'zh-Hans': [{'ext': 'srt', 'name': '简体中文'}]},
        'automatic_captions': {'en': [{'ext': 'vtt', 'name': '英语'}]},
    }))
    raise SystemExit(0)
if 'BVcancel0000' in sys.argv[-1]:
    time.sleep(120)
if 'BVfailed0000' in sys.argv[-1]:
    print('ERROR: HTTP 412 test platform restriction', file=sys.stderr)
    raise SystemExit(1)
if '--dump-single-json' in sys.argv:
    if 'www.xiaohongshu.com/explore/' in sys.argv[-1]:
        from urllib.parse import urlparse, parse_qs
        target = urlparse(sys.argv[-1])
        video_id = target.path.split('/')[-1]
        assert parse_qs(target.query).get('xsec_token') == ['fixture-access']
        assert video_id == '64aaaaaaaaaaaaaaaaaaaaaa'
        assert '--skip-download' in sys.argv and '--format' in sys.argv
        if '--cookies' in sys.argv:
            jar = pathlib.Path(sys.argv[sys.argv.index('--cookies')+1]).read_text()
            assert '.xiaohongshu.com' in jar and 'youtube-private' not in jar
        print(json.dumps({'id':video_id,'protocol':'https','url':'https://fixture.xhscdn.com/audio.mp4','duration':12}))
        raise SystemExit(0)
    if 'www.douyin.com/video/' in sys.argv[-1]:
        video_id = sys.argv[-1].rstrip('/').split('/')[-1]
        assert '--skip-download' in sys.argv and '--no-playlist' in sys.argv
        if video_id == '6982497745948921092' and '--cookies' not in sys.argv:
            print('Fresh cookies required synthetic-private-douyin-secret', file=sys.stderr)
            raise SystemExit(1)
        if video_id == '6953975910773099811':
            time.sleep(120)
        if '--cookies' in sys.argv:
            jar = pathlib.Path(sys.argv[sys.argv.index('--cookies')+1]).read_text()
            assert '.douyin.com' in jar and 'youtube-private' not in jar
        data = {'id':video_id,'title':'抖音单视频测试夹具','uploader':'抖音测试作者','uploader_id':'123456','duration':12,'timestamp':1790812800,'description':'明确标识的单视频测试内容','formats':[{'secret':'never forwarded'}]}
        if '--format' in sys.argv:
            data.update({'protocol':'https','url':'https://fixture.douyinvod.com/audio.mp4'})
        print(json.dumps(data))
        raise SystemExit(0)
    if '--playlist-items' in sys.argv:
        assert sys.argv[sys.argv.index('--playlist-items')+1]=='0' and '--flat-playlist' in sys.argv and '--simulate' in sys.argv and '--skip-download' in sys.argv
        assert '--cookies' not in sys.argv and '--paths' not in sys.argv
        target = sys.argv[-1]
        if '@channel-fail' in target:
            print('ERROR synthetic-private-channel-cookie', file=sys.stderr)
            raise SystemExit(1)
        if '@channel-slow' in target:
            time.sleep(120)
        channel_id = 'UC'+'b'*22 if '@channel-second' in target else 'UC'+'e'*22 if '@channel-empty' in target else 'UC'+'a'*22
        print(json.dumps({'_type':'playlist','channel_id':channel_id,'channel':'YouTube 频道测试','channel_url':'https://www.youtube.com/channel/'+channel_id,'entries':[]}))
        raise SystemExit(0)
    # Explicitly fake existing captions. This branch never creates media or subtitle files.
    video_id = sys.argv[-1].rstrip('/').split('/')[-1]
    if 'youtube.com/watch' in sys.argv[-1]:
        from urllib.parse import urlparse, parse_qs
        video_id = parse_qs(urlparse(sys.argv[-1]).query)['v'][0]
        langs = ['en'] if video_id == 'ENonly00000' else [] if video_id in ['Nocaps00000', 'Nocaps00001'] else ['en', 'zh-Hans']
        tracks = {lang: [{'ext':'json3','url':'https://www.youtube.com/api/timedtext?lang='+lang,'name':lang}] for lang in langs}
        data = {'id':video_id,'title':'YouTube 视频测试夹具','uploader':'YouTube 测试作者','duration':12,'description':'明确标识的 YouTube 测试简介','subtitles':tracks,'automatic_captions':{}}
        if '--format' in sys.argv:
            data.update({'protocol':'https','url':'https://fixture.googlevideo.com/audio.m4a'})
        if '--write-subs' in sys.argv:
            assert '--no-simulate' in sys.argv and '--skip-download' in sys.argv
            lang = sys.argv[sys.argv.index('--sub-langs')+1].strip('^$').replace('\\-', '-')
            directory = pathlib.Path(sys.argv[sys.argv.index('--paths')+1])
            file = directory / ('caption.'+lang+'.json3')
            text = '中文优先测试字幕' if lang == 'zh-Hans' else 'English subtitle fixture'
            file.write_text(json.dumps({'events':[{'tStartMs':1250,'dDurationMs':2750,'segs':[{'utf8':text}]},{'tStartMs':5000,'dDurationMs':3500,'segs':[{'utf8':'Second caption & plain text'}]}]}))
            data['requested_subtitles'] = {lang:{'ext':'json3','filepath':str(file),'url':'https://www.youtube.com/api/timedtext?lang='+lang}}
        print(json.dumps(data))
        raise SystemExit(0)
    tracks = {'danmaku': [{'ext': 'xml', 'url': 'https://example.invalid/not-requested'}]}
    if video_id == 'BVlogin00000':
        print('WARNING: [BiliBili] Subtitles are only available when logged in', file=sys.stderr)
    elif video_id != 'BVnone000000':
        tracks['ai-zh'] = [{'ext': 'srt', 'data': '1\n00:00:01,250 --> 00:00:04,000\n第一段测试字幕：好奇心。\n\n2\n00:00:05,000 --> 00:00:08,500\n第二段测试字幕：继续发现。\n\n3\n00:00:09,000 --> 00:00:11,000\n<script>这只是字幕文本</script>\n'}]
        tracks['en'] = [{'ext': 'srt', 'data': '1\n00:00:01,250 --> 00:00:04,000\nEnglish subtitle fixture.\n'}]
        if video_id == 'BVlong000000':
            tracks['ai-zh'] = [{'ext': 'srt', 'data': ''.join(f'{i+1}\n00:{i//60:02}:{i%60:02},000 --> 00:{(i+1)//60:02}:{(i+1)%60:02},000\n第{i}段长字幕测试\n\n' for i in range(250))}]
        if video_id == 'BVfollow0000':
            def clock_ms(value):
                return f'00:00:{value // 1000:02},{value % 1000:03}'
            tracks['ai-zh'] = [{'ext': 'srt', 'data': ''.join(f'{i+1}\n{clock_ms(i*40)} --> {clock_ms((i+1)*40)}\n第{i}段跟随测试\n\n' for i in range(250))}]
    print(json.dumps({'id': video_id + '_p1', 'subtitles': tracks}))
    raise SystemExit(0)
directory = pathlib.Path(sys.argv[sys.argv.index('--paths') + 1])
directory.mkdir(parents=True, exist_ok=True)
from urllib.parse import urlparse, parse_qs
target = urlparse(sys.argv[-1])
video_id = parse_qs(target.query).get('v', [target.path.rstrip('/').split('/')[-1]])[0]
title = video_id if video_id.startswith('BV') else '下载测试视频'
ext = sys.argv[sys.argv.index('--remux-video') + 1] if '--remux-video' in sys.argv else 'mp4'
if '--extract-audio' in sys.argv:
    ext = sys.argv[sys.argv.index('--audio-format') + 1]
    if ext == 'best':
        ext = 'm4a'
file = directory / (title + '.' + ext)
if '--write-subs' in sys.argv:
    for language in sys.argv[sys.argv.index('--sub-langs') + 1].split(','):
        language = language.strip('^$').replace('\\-', '-')
        (directory / (title + '.' + language + '.srt')).write_text('1\n00:00:00,000 --> 00:00:01,000\n字幕测试\n')
if any(video in sys.argv[-1] for video in ['BVplayer0000', 'BVfollow0000', 'BVresume0000', 'BVconflict00', 'YTplayer000', '6961737553342991651', '64aaaaaaaaaaaaaaaaaaaaaa']):
    shutil.copyfile(pathlib.Path(__file__).parent / 'fixtures' / 'player.mp4', file)
else:
    file.write_bytes(b'explicit e2e test fixture, not playable media')
print(file.resolve())
