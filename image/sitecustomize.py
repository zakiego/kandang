"""Kandang's chart defaults that a matplotlibrc can't hold.

Bars get a narrower default width and pies start at 12 o'clock, go clockwise
and have a thin gap in the surface color between slices, with white percent
labels. Only defaults change: anything the code passes itself wins.

It waits until a run imports matplotlib.pyplot, so a run that never plots
doesn't pay for importing matplotlib, and any error here leaves matplotlib as
it was instead of breaking the run.
"""

import importlib.abc
import importlib.util
import sys

BAR_WIDTH = 0.62


def _apply(pyplot):
    import functools

    import matplotlib
    from matplotlib.axes import Axes

    axes_bar, axes_barh, axes_pie = Axes.bar, Axes.barh, Axes.pie

    @functools.wraps(axes_bar)
    def bar(self, x, height, width=BAR_WIDTH, *args, **kwargs):
        return axes_bar(self, x, height, width, *args, **kwargs)

    @functools.wraps(axes_barh)
    def barh(self, y, width, height=BAR_WIDTH, *args, **kwargs):
        return axes_barh(self, y, width, height, *args, **kwargs)

    @functools.wraps(axes_pie)
    def pie(self, x, *args, **kwargs):
        # pyplot.pie passes its own defaults through, so None (and the stock
        # values) count as "not set".
        if kwargs.get("wedgeprops") is None:
            kwargs["wedgeprops"] = {"linewidth": 2.5, "edgecolor": matplotlib.rcParams["axes.facecolor"]}
        if kwargs.get("textprops") is None:
            kwargs["textprops"] = {"color": matplotlib.rcParams["text.color"]}
        if kwargs.get("startangle") in (None, 0):
            kwargs["startangle"] = 90
        if kwargs.get("counterclock") in (None, True):
            kwargs["counterclock"] = False
        if kwargs.get("pctdistance") in (None, 0.6):
            kwargs["pctdistance"] = 0.72
        result = axes_pie(self, x, *args, **kwargs)
        if len(result) == 3:
            for text in result[2]:
                text.set_color("white")
                text.set_fontweight("semibold")
        return result

    Axes.bar, Axes.barh, Axes.pie = bar, barh, pie

    # pyplot's own bar and barh pass width=0.8 explicitly, so they get the
    # new default too.
    pyplot_bar, pyplot_barh = pyplot.bar, pyplot.barh

    @functools.wraps(pyplot_bar)
    def plt_bar(x, height, width=BAR_WIDTH, *args, **kwargs):
        return pyplot.gca().bar(x, height, width, *args, **kwargs)

    @functools.wraps(pyplot_barh)
    def plt_barh(y, width, height=BAR_WIDTH, *args, **kwargs):
        return pyplot.gca().barh(y, width, height, *args, **kwargs)

    pyplot.bar, pyplot.barh = plt_bar, plt_barh


class _PyplotHook(importlib.abc.MetaPathFinder):
    def find_spec(self, name, path, target=None):
        if name != "matplotlib.pyplot":
            return None
        sys.meta_path.remove(self)
        spec = importlib.util.find_spec(name)
        if spec is None or spec.loader is None:
            return spec
        exec_module = spec.loader.exec_module

        def exec_and_style(module):
            exec_module(module)
            try:
                _apply(module)
            except Exception:
                pass

        spec.loader.exec_module = exec_and_style
        return spec


sys.meta_path.insert(0, _PyplotHook())
