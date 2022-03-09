# ircd.chat

Oragono's grumpy little brother.

### Changes:
 - [x] History gutted
 - [x] MySQL erradicated
 - [x] Diverge from lots of "override" logic
 - [x] Handle errors that were previously ignored
 - [ ] Accurately document all of the changes
 
## Bloat Comparison

### Ergo
```
Examining 379 file(s)
                          Ohloh Line Count Summary                          

Language          Files       Code    Comment  Comment %      Blank      Total
----------------  -----  ---------  ---------  ---------  ---------  ---------
golang              132      27850       2936       9.5%       4305      35091
python                4        679         92      11.9%         88        859
shell                 3         66         13      16.5%         18         97
xml                   1         48          0       0.0%          0         48
ampl                  1         39          0       0.0%          6         45
make                  1         38          0       0.0%         11         49
----------------  -----  ---------  ---------  ---------  ---------  ---------
Total               142      28720       3041       9.6%       4428      36189
```

### ircd.chat
```
Examining 162 file(s)
                          Ohloh Line Count Summary                          

Language          Files       Code    Comment  Comment %      Blank      Total
----------------  -----  ---------  ---------  ---------  ---------  ---------
golang              122      24886       2718       9.8%       3895      31499
python                1        209         18       7.9%         16        243
shell                 3         66         13      16.5%         18         97
ampl                  1         38          0       0.0%          6         44
make                  1         36          0       0.0%         11         47
----------------  -----  ---------  ---------  ---------  ---------  ---------
Total               128      25235       2749       9.8%       3946      31930
```

### As of writing, **3,485 lines of code have been removed from ergo**.
