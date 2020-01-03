import ExpandMoreIcon from '@material-ui/icons/ExpandMore';
import ExpansionPanel from '@material-ui/core/ExpansionPanel';
import ExpansionPanelDetails from '@material-ui/core/ExpansionPanelDetails';
import ExpansionPanelSummary from '@material-ui/core/ExpansionPanelSummary';
import PropTypes from 'prop-types';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  root: {
    width: '100%',
  },
  heading: {
    fontSize: theme.typography.pxToRem(15),
    flexBasis: '33.33%',
    flexShrink: 0,
  },
  secondaryHeading: {
    fontSize: theme.typography.pxToRem(15),
    color: theme.palette.text.secondary,
  },
});

class Accordion extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      expanded: props.defaultOpenPanel || null,
    };
  }

  handlePanelSelect = panel => (_event, expanded) => {
    const { onChange } = this.props;

    this.setState({
      expanded: expanded ? panel : false,
    });

    if (expanded) {
      onChange(panel);
    }
  };

  render() {
    const { classes, panels } = this.props;
    const { expanded } = this.state;

    return (
      <div className={classes.root}>
        {
          panels.map(panel => (
            <ExpansionPanel
              elevation={3}
              expanded={expanded === panel.id}
              onChange={this.handlePanelSelect(panel.id)}
              key={panel.id}>
              <ExpansionPanelSummary expandIcon={<ExpandMoreIcon />}>
                {panel.header}
              </ExpansionPanelSummary>
              <ExpansionPanelDetails>
                {panel.body || 'No data present.'}
              </ExpansionPanelDetails>
            </ExpansionPanel>
          ))
        }
      </div>
    );
  }
}

Accordion.propTypes = {
  defaultOpenPanel: PropTypes.string,
  onChange: PropTypes.func.isRequired,
  panels: PropTypes.arrayOf(PropTypes.shape({
    id: PropTypes.string,
    header: PropTypes.node,
    body: PropTypes.node,
  })),
};

Accordion.defaultProps = {
  defaultOpenPanel: null,
  panels: [],
};

export default withStyles(styles)(Accordion);
