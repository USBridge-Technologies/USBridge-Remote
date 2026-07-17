//go:build darwin && !ios

#import <AppKit/AppKit.h>
#include <string.h>
#include <stdlib.h>

// g_lastFileURLs caches the file URLs found by the most recent
// clipboard_get_file_count() call so clipboard_get_file_path() can index
// into them without re-querying the pasteboard per file. Safe because the Go
// side only ever drives this from a single goroutine (clipboard.Manager's
// poll loop).
static NSArray<NSURL *> *g_lastFileURLs = nil;

int clipboard_change_count(void) {
    @autoreleasepool {
        return (int)[[NSPasteboard generalPasteboard] changeCount];
    }
}

char *clipboard_get_text(void) {
    @autoreleasepool {
        NSString *s = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
        if (!s) {
            return NULL;
        }
        const char *utf8 = s.UTF8String;
        if (!utf8) {
            return NULL;
        }
        return strdup(utf8);
    }
}

int clipboard_set_text(const char *text) {
    @autoreleasepool {
        NSString *s = [NSString stringWithUTF8String:text];
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        return [pb setString:s forType:NSPasteboardTypeString] ? 1 : 0;
    }
}

unsigned char *clipboard_get_image_png(size_t *outLen) {
    @autoreleasepool {
        *outLen = 0;
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSData *data = [pb dataForType:@"public.png"];
        if (!data) {
            NSData *tiff = [pb dataForType:NSPasteboardTypeTIFF];
            if (tiff) {
                NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithData:tiff];
                data = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
            }
        }
        if (!data) {
            return NULL;
        }
        size_t len = data.length;
        unsigned char *buf = malloc(len);
        if (!buf) {
            return NULL;
        }
        memcpy(buf, data.bytes, len);
        *outLen = len;
        return buf;
    }
}

int clipboard_set_image_png(const unsigned char *data, size_t len) {
    @autoreleasepool {
        NSData *pngData = [NSData dataWithBytes:data length:len];
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        BOOL ok = [pb setData:pngData forType:@"public.png"];
        // Also provide a TIFF representation for apps that only understand
        // the classic NSPasteboardTypeTIFF image type.
        NSImage *image = [[NSImage alloc] initWithData:pngData];
        if (image) {
            NSData *tiffData = [image TIFFRepresentation];
            if (tiffData) {
                [pb setData:tiffData forType:NSPasteboardTypeTIFF];
            }
        }
        return ok ? 1 : 0;
    }
}

int clipboard_get_file_count(void) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSArray *urls = [pb readObjectsForClasses:@[[NSURL class]]
                                           options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}];
        g_lastFileURLs = urls;
        return (int)urls.count;
    }
}

char *clipboard_get_file_path(int index) {
    @autoreleasepool {
        if (!g_lastFileURLs || index < 0 || index >= (int)g_lastFileURLs.count) {
            return NULL;
        }
        NSURL *url = g_lastFileURLs[index];
        const char *path = url.path.UTF8String;
        if (!path) {
            return NULL;
        }
        return strdup(path);
    }
}

int clipboard_set_files(const char **paths, int count) {
    @autoreleasepool {
        NSMutableArray<NSURL *> *urls = [NSMutableArray arrayWithCapacity:count];
        for (int i = 0; i < count; i++) {
            NSString *p = [NSString stringWithUTF8String:paths[i]];
            NSURL *url = [NSURL fileURLWithPath:p];
            if (url) {
                [urls addObject:url];
            }
        }
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        return [pb writeObjects:urls] ? 1 : 0;
    }
}
