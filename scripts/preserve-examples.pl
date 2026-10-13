#!/usr/bin/env perl
use strict;
use warnings;

# Scribe regenerates sample IDs, dates, and enum choices on every deploy.
# Preserve committed examples while accepting other live spec changes.
my $spec_path = shift @ARGV or die "usage: preserve-examples.pl openapi.yaml < baseline\n";
my @baseline = <STDIN>;
die "missing committed OpenAPI baseline\n" unless @baseline;
open my $spec_file, '<', $spec_path or die "$spec_path: $!";
my @current = <$spec_file>;
close $spec_file;

sub examples {
    my ($lines) = @_;
    my (%found, @stack, %next_index);
    my $i = 0;
    while ($i < @$lines) {
        my $line = $lines->[$i];
        if ($line =~ /^([ ]*)(?:- )?example:(?:\s|$)/) {
            my $indent = length($1);
            pop @stack while @stack && $stack[-1][0] >= $indent;
            my $path = join '/', map { $_->[1] } @stack;
            $path .= '/example';
            my $end = $i + 1;
            while ($end < @$lines &&
                   ($lines->[$end] =~ /^\s*$/ ||
                    $lines->[$end] =~ /^([ ]*)\S/ && length($1) > $indent)) {
                $end++;
            }
            die "duplicate example path: $path\n" if exists $found{$path};
            $found{$path} = [$i, $end, [@$lines[$i .. $end - 1]]];
            $i = $end;
            next;
        }
        if ($line =~ /^([ ]*)(-)(?:\s+(.*))?$/) {
            my ($indent, $item) = (length($1), $3 // '');
            pop @stack while @stack && $stack[-1][0] >= $indent;
            my $parent = join '/', map { $_->[1] } @stack;
            my $counter = $parent . '@' . $indent;
            my $index = $next_index{$counter}++;
            push @stack, [$indent, "[$index]"];
            push @stack, [$indent + 2, $1] if $item =~ /^([^:]+):(?:\s|$)/;
        } elsif ($line =~ /^([ ]*)([^:#][^:]*):(?:\s|$)/) {
            my ($indent, $key) = (length($1), $2);
            pop @stack while @stack && $stack[-1][0] >= $indent;
            push @stack, [$indent, $key];
        }
        $i++;
    }
    return %found;
}

my %old = examples(\@baseline);
my %new = examples(\@current);
my @replacements;
for my $path (keys %new) {
    next unless exists $old{$path};
    my ($start, $end) = @{$new{$path}};
    push @replacements, [$start, $end, $old{$path}[2]];
}
for my $replacement (sort { $b->[0] <=> $a->[0] } @replacements) {
    my ($start, $end, $lines) = @$replacement;
    splice @current, $start, $end - $start, @$lines;
}
open my $out, '>', $spec_path or die "$spec_path: $!";
print {$out} @current;
close $out or die "$spec_path: $!";
