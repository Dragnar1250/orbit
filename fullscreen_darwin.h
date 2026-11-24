#pragma once
void *fs_open(double x, double y, double w, double h);
void fs_show(OG *g);
void fs_close(void);
int fs_in_front(void);
void fs_refocus(void);
void fs_cursor(int show);
void fs_video_open(const char *url);
void fs_video_close(void);
int fs_video_open_now(void);
int os_popup(const char **items, int n, int checked);
int os_open_url(const char *u);
