import { OctopusArms, inboundAlignment } from './util/OctopusArms.jsx';
import { displayName, metricToFormatter } from './util/Utils.js';

import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import RootRef from '@material-ui/core/RootRef';
import { StyledProgress } from './util/Progress.jsx';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import TableCell from '@material-ui/core/TableCell';
import TableRow from '@material-ui/core/TableRow';
import Typography from '@material-ui/core/Typography';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isNil from 'lodash/isNil';
import _size from 'lodash/size';
import _slice from 'lodash/slice';
import _sortBy from 'lodash/sortBy';
import _take from 'lodash/take';
import { getSuccessRateClassification } from './util/MetricUtils.jsx';
import { withStyles } from '@material-ui/core/styles';

const maxNumNeighbors = 6; // max number of neighbor nodes to show in the octopus graph

const styles = () => ({
  graphContainer: {
    overflowX: 'auto',
    padding: '16px 0',
  },
  graph: {
    maxWidth: '974px',
    minWidth: '974px',
    marginLeft: 'auto',
    marginRight: 'auto',
  },
  centerNode: {
    width: '244px',
  },
  neighborNode: {
    width: '220px',
  },
  collapsedNeighborName: {
    paddingTop: '10px',
  },
});
class Octopus extends React.Component {
  constructor(props) {
    super(props);

    this.upstreamsContainer = React.createRef();
    this.downstreamsContainer = React.createRef();
    this.upstreamsRefs = [];
    this.downstreamsRefs = [];
  }

  getNeighborDisplayData = neighbors => {
    // only display maxNumNeighbors neighboring nodes in the octopus graph,
    // otherwise it will be really tall
    // even though _sortBy is a stable sort, the order that this data is returned by the API
    // can change, so the original order of items can change; this means we have to sort by
    // name and by SR to ensure an actual stable sort
    const upstreams = _sortBy(_sortBy(neighbors.upstream, displayName), n => n.successRate);
    const downstreams = _sortBy(_sortBy(neighbors.downstream, displayName), n => n.successRate);

    const display = {
      upstreams: {
        displayed: upstreams,
        collapsed: [],
      },
      downstreams: {
        displayed: downstreams,
        collapsed: [],
      },
    };

    if (_size(upstreams) > maxNumNeighbors) {
      display.upstreams.displayed = _take(upstreams, maxNumNeighbors);
      display.upstreams.collapsed = _slice(upstreams, maxNumNeighbors, _size(upstreams));
    }

    if (_size(downstreams) > maxNumNeighbors) {
      display.downstreams.displayed = _take(downstreams, maxNumNeighbors);
      display.downstreams.collapsed = _slice(downstreams, maxNumNeighbors, _size(downstreams));
    }

    return display;
  }

  addElementToRefsList = (element, index, isOutbound, type = 'neighbor') => {
    if (element && (type !== 'main')) {
      if (isOutbound) {
        this.downstreamsRefs[index] = element;
      } else {
        this.upstreamsRefs[index] = element;
      }
    }
  }

  linkedResourceTitle = (resource, display) => {
    // trafficsplit leaf resources cannot be linked
    if (_isNil(resource.namespace) || resource.isLeafService) { return display; }

    const { api: { ResourceLink } } = this.props;
    return <ResourceLink resource={resource} linkText={display} />;
  }

  renderResourceCard(resource, type, index, isOutbound) {
    const { classes } = this.props;
    const display = displayName(resource);
    const classification = getSuccessRateClassification(resource.successRate);
    const Progress = StyledProgress(classification);

    // if the resource only has TCP stats, display those instead
    let showTcp = false;
    // trafficsplit leaf with zero traffic should still show HTTP stats
    if (_isNil(resource.successRate) && _isNil(resource.requestRate) &&
      resource.type !== 'service') {
      showTcp = true;
    }

    return (
      <RootRef rootRef={el => this.addElementToRefsList(el, index, isOutbound, type)} key={`${resource.type}-${resource.name}`}>
        <Grid item>
          <Card className={type === 'neighbor' ? classes.neighborNode : classes.centerNode} title={display}>
            <CardContent>

              <Typography variant={type === 'neighbor' ? 'subtitle1' : 'h6'} align="center">
                { this.linkedResourceTitle(resource, display) }
              </Typography>

              <Progress variant="determinate" value={resource.successRate * 100} />

              <Table>
                {showTcp ? this.renderTCPStats(resource) : this.renderHttpStats(resource)}
              </Table>
            </CardContent>
          </Card>
        </Grid>
      </RootRef>
    );
  }

  renderHttpStats = resource => {
    return (
      <TableBody>
        {resource.isLeafService &&
        <TableRow>
          <TableCell><Typography>Weight</Typography></TableCell>
          <TableCell align="right"><Typography>{resource.tsStats.weight}</Typography></TableCell>
        </TableRow>
        }
        <TableRow>
          <TableCell><Typography>SR</Typography></TableCell>
          <TableCell align="right"><Typography>{metricToFormatter.SUCCESS_RATE(resource.successRate)}</Typography></TableCell>
        </TableRow>
        <TableRow>
          <TableCell><Typography>RPS</Typography></TableCell>
          <TableCell align="right"><Typography>{metricToFormatter.NO_UNIT(resource.requestRate)}</Typography></TableCell>
        </TableRow>
        {!resource.isApexService &&
        <TableRow>
          <TableCell><Typography>P99</Typography></TableCell>
          <TableCell align="right"><Typography>{metricToFormatter.LATENCY(_get(resource, 'latency.P99'))}</Typography></TableCell>
        </TableRow>
        }
      </TableBody>
    );
  }

  renderTCPStats = resource => {
    const { tcp } = resource;
    return (
      <TableBody>
        <TableRow>
          <TableCell><Typography>Conn</Typography></TableCell>
          <TableCell align="right"><Typography>{metricToFormatter.NO_UNIT(tcp.openConnections)}</Typography></TableCell>
        </TableRow>
        <TableRow>
          <TableCell><Typography>Read</Typography></TableCell>
          <TableCell align="right"><Typography>{metricToFormatter.BYTES(tcp.readRate)}</Typography></TableCell>
        </TableRow>
        <TableRow>
          <TableCell><Typography>Write</Typography></TableCell>
          <TableCell align="right"><Typography>{metricToFormatter.BYTES(tcp.writeRate)}</Typography></TableCell>
        </TableRow>
      </TableBody>
    );
  }

  renderUnmeshedResources = (unmeshedResources, index, isOutbound) => {
    const { classes } = this.props;
    return (
      <RootRef rootRef={el => this.addElementToRefsList(el, index, isOutbound)}>
        <Grid item>
          <Card key="unmeshed-resources" className={classes.neighborNode}>
            <CardContent>
              <Typography variant="subtitle1">Unmeshed</Typography>
              {
                unmeshedResources.map(r => {
                  const display = displayName(r);
                  return <Typography key={display} variant="body2" title={display}>{display}</Typography>;
                })
              }
            </CardContent>
          </Card>
        </Grid>
      </RootRef>
    );
  }

  renderCollapsedNeighbors = (neighbors, index, isOutbound) => {
    const { classes } = this.props;
    const Progress = StyledProgress();
    return (
      <RootRef rootRef={el => this.addElementToRefsList(el, index, isOutbound)}>
        <Grid item>
          <Card className={classes.neighborNode}>
            <CardContent>
              <Typography variant="subtitle1">
                + { neighbors.length } more...
              </Typography>
              <Progress variant="determinate" value={100} />
              {
                neighbors.map(r => {
                  const display = displayName(r);
                  return <Typography key={display} className={classes.collapsedNeighborName}>{this.linkedResourceTitle(r, display)}</Typography>;
                })
              }
            </CardContent>
          </Card>
        </Grid>
      </RootRef>
    );
  }

  renderArrowCol = (numNeighbors, isOutbound) => {
    const container = !isOutbound ? this.upstreamsContainer : this.downstreamsContainer;
    const refs = !isOutbound ? this.upstreamsRefs : this.downstreamsRefs;
    if (refs.length === 0 || container.current === undefined) {
      return null;
    }

    const width = 80;
    const fullHeight = container.current.offsetHeight;
    let arrowTypes = [];
    arrowTypes = refs.slice(0, numNeighbors).map(element => {
      const elementTop = element.offsetTop - container.current.offsetTop;
      const elementHeight = element.offsetHeight;
      const halfElement = elementHeight / 2;

      // Middle element
      if (Math.round(fullHeight / 2) === Math.round(elementTop + halfElement)) {
        const height = elementTop + halfElement;
        return { type: 'flat', inboundType: 'flat', height };

      // Elements underneath main element
      } else if (elementTop + halfElement >= fullHeight / 2) {
        const height = !isOutbound ? elementTop - fullHeight / 2 + halfElement - inboundAlignment : fullHeight - elementTop - halfElement + inboundAlignment;
        return { type: 'down', inboundType: 'up', height, elementHeight };

      // Elements over main element
      } else {
        const height = !isOutbound ? elementTop + halfElement + inboundAlignment : fullHeight / 2 - elementTop - halfElement - inboundAlignment;
        return { type: 'up', inboundType: 'down', height, elementHeight };
      }
    });

    const svg = (
      <svg height={fullHeight} width={width} version="1.1" viewBox={`0 0 ${width} ${fullHeight}`}>
        <defs />
        {
          arrowTypes.map(arrow => {
            const arrowType = isOutbound ? arrow.type : arrow.inboundType;
            return OctopusArms[arrowType](width, fullHeight, arrow.height, isOutbound, arrow.elementHeight);
          })
        }
      </svg>
    );

    return svg;
  }

  render() {
    const { resource, neighbors, unmeshedSources, classes } = this.props;

    if (_isEmpty(resource)) {
      return null;
    }

    const display = this.getNeighborDisplayData(neighbors);

    const numUpstreams = _size(display.upstreams.displayed) + (_isEmpty(unmeshedSources) ? 0 : 1) +
       (_isEmpty(display.upstreams.collapsed) ? 0 : 1);

    const numDownstreams = _size(display.downstreams.displayed) + (_isEmpty(display.downstreams.collapsed) ? 0 : 1);

    return (
      <div className={classes.graphContainer}>
        <div className={classes.graph}>
          <Grid
            container
            direction="row"
            justify="center"
            alignItems="center">

            <RootRef rootRef={this.upstreamsContainer}>
              <Grid
                container
                spacing={3}
                direction="column"
                justify="center"
                alignItems="center"
                item
                xs={3}>
                {display.upstreams.displayed.map((n, index) => this.renderResourceCard(n, 'neighbor', index, false))}
                {_isEmpty(unmeshedSources) ? null : this.renderUnmeshedResources(unmeshedSources, display.upstreams.displayed.length, false)}
                {_isEmpty(display.upstreams.collapsed) ? null : this.renderCollapsedNeighbors(display.upstreams.collapsed, _isEmpty(unmeshedSources) ? display.upstreams.displayed.length : display.upstreams.displayed.length + 1, false)}
              </Grid>
            </RootRef>

            <Grid item xs={1}>
              {this.renderArrowCol(numUpstreams, false)}
            </Grid>

            <Grid item xs={3}>
              {this.renderResourceCard(resource, 'main')}
            </Grid>

            <Grid item xs={1}>
              {this.renderArrowCol(numDownstreams, true)}
            </Grid>

            <RootRef rootRef={this.downstreamsContainer}>
              <Grid
                container
                spacing={3}
                direction="column"
                justify="center"
                alignItems="center"
                item
                xs={3}>
                {display.downstreams.displayed.map((n, index) => this.renderResourceCard(n, 'neighbor', index, true))}
                {_isEmpty(display.downstreams.collapsed) ? null : this.renderCollapsedNeighbors(display.downstreams.collapsed, display.downstreams.displayed.length, true)}
              </Grid>
            </RootRef>
          </Grid>
        </div>
      </div>
    );
  }
}

Octopus.propTypes = {
  api: PropTypes.shape({
    ResourceLink: PropTypes.oneOfType([PropTypes.element, PropTypes.func]),
  }),
  neighbors: PropTypes.shape({}),
  resource: PropTypes.shape({}),
  unmeshedSources: PropTypes.arrayOf(PropTypes.shape({})),
};

Octopus.defaultProps = {
  api: null,
  neighbors: {},
  resource: {},
  unmeshedSources: [],
};

export default withStyles(styles)(Octopus);
